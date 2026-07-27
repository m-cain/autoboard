import type { FromSchema } from "json-schema-to-ts";
import {
  ActivityEventSchema,
  ProjectBoardSchema,
  ProjectListSchema,
  TicketDetailSchema,
  TicketListSchema,
} from "./generated/browser-schemas.js";

export type BrowserActivityEvent = FromSchema<typeof ActivityEventSchema>;
export type BrowserProjectBoard = FromSchema<typeof ProjectBoardSchema>;
export type BrowserProjectList = FromSchema<typeof ProjectListSchema>;
export type BrowserTicketDetail = FromSchema<typeof TicketDetailSchema>;
export type BrowserTicketList = FromSchema<typeof TicketListSchema>;
export type BrowserProject = BrowserProjectList["active"][number];
export type BrowserTicketSummary = BrowserTicketList["tickets"][number];
export type ActivityEvent = BrowserActivityEvent;
export type ProjectBoard = BrowserProjectBoard;
export type ProjectList = BrowserProjectList;
export type TicketDetail = BrowserTicketDetail;
export type TicketList = BrowserTicketList;
export type Project = BrowserProject;
export type TicketSummary = BrowserTicketSummary;

type SchemaNode =
  | boolean
  | {
      $ref?: string;
      definitions?: Readonly<Record<string, SchemaNode>>;
      oneOf?: readonly SchemaNode[];
      enum?: readonly unknown[];
      type?: string;
      required?: readonly string[];
      properties?: Readonly<Record<string, SchemaNode>>;
      additionalProperties?: boolean;
      items?: SchemaNode | readonly SchemaNode[];
      minimum?: number;
      minLength?: number;
      maxLength?: number;
      pattern?: string;
      format?: string;
    };

type SchemaObject = Exclude<SchemaNode, boolean>;

const decoder = <A>(schema: SchemaObject): ((value: unknown) => A) => {
  return (value) => {
    const failure = validateSchema(schema, value, schema, "$");
    if (failure) {
      throw new Error(failure);
    }
    return value as A;
  };
};

const validateSchema = (
  schema: SchemaNode,
  value: unknown,
  root: SchemaObject,
  location: string,
): string | undefined => {
  if (schema === true) return undefined;
  if (schema === false) return `${location} is forbidden`;
  if (schema.$ref) {
    const prefix = "#/definitions/";
    if (!schema.$ref.startsWith(prefix)) {
      return `${location} uses unsupported schema reference ${schema.$ref}`;
    }
    const definition = root.definitions?.[schema.$ref.slice(prefix.length)];
    if (!definition) {
      return `${location} references a missing schema`;
    }
    return validateSchema(definition, value, root, location);
  }
  if (schema.oneOf) {
    const matches = schema.oneOf.filter(
      (candidate) =>
        validateSchema(candidate, value, root, location) === undefined,
    );
    return matches.length === 1
      ? undefined
      : `${location} must match exactly one allowed shape`;
  }
  if (schema.enum && !schema.enum.includes(value as never)) {
    return `${location} must be one of ${schema.enum.join(", ")}`;
  }
  switch (schema.type) {
    case "null":
      return value === null ? undefined : `${location} must be null`;
    case "boolean":
      return typeof value === "boolean"
        ? undefined
        : `${location} must be a boolean`;
    case "integer":
      if (!Number.isInteger(value)) return `${location} must be an integer`;
      return validateNumber(schema, value as number, location);
    case "number":
      if (typeof value !== "number" || !Number.isFinite(value)) {
        return `${location} must be a finite number`;
      }
      return validateNumber(schema, value, location);
    case "string":
      return validateString(schema, value, location);
    case "array":
      if (!Array.isArray(value)) return `${location} must be an array`;
      if (!schema.items || typeof schema.items === "boolean") {
        return schema.items === false && value.length > 0
          ? `${location} must be empty`
          : undefined;
      }
      if (Array.isArray(schema.items)) {
        return `${location} uses unsupported tuple validation`;
      }
      const itemSchema = schema.items as SchemaNode;
      for (const [index, item] of value.entries()) {
        const failure = validateSchema(
          itemSchema,
          item,
          root,
          `${location}[${index}]`,
        );
        if (failure) return failure;
      }
      return undefined;
    case "object":
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        return `${location} must be an object`;
      }
      return validateObject(
        schema,
        value as Record<string, unknown>,
        root,
        location,
      );
    case undefined:
      return undefined;
    default:
      return `${location} uses unsupported schema type`;
  }
};

const validateObject = (
  schema: SchemaObject,
  value: Record<string, unknown>,
  root: SchemaObject,
  location: string,
): string | undefined => {
  for (const required of schema.required ?? []) {
    if (!Object.hasOwn(value, required)) {
      return `${location}.${required} is required`;
    }
  }
  const properties = schema.properties ?? {};
  if (schema.additionalProperties === false) {
    for (const property of Object.keys(value)) {
      if (!Object.hasOwn(properties, property)) {
        return `${location}.${property} is not allowed`;
      }
    }
  }
  for (const [property, propertySchema] of Object.entries(properties)) {
    if (!Object.hasOwn(value, property)) continue;
    const failure = validateSchema(
      propertySchema,
      value[property],
      root,
      `${location}.${property}`,
    );
    if (failure) return failure;
  }
  return undefined;
};

const validateNumber = (
  schema: SchemaObject,
  value: number,
  location: string,
): string | undefined => {
  if (schema.minimum !== undefined && value < schema.minimum) {
    return `${location} must be at least ${schema.minimum}`;
  }
  return undefined;
};

const validateString = (
  schema: SchemaObject,
  value: unknown,
  location: string,
): string | undefined => {
  if (typeof value !== "string") return `${location} must be a string`;
  if (schema.minLength !== undefined && value.length < schema.minLength) {
    return `${location} is too short`;
  }
  if (schema.maxLength !== undefined && value.length > schema.maxLength) {
    return `${location} is too long`;
  }
  if (schema.pattern && !new RegExp(schema.pattern, "u").test(value)) {
    return `${location} has an invalid format`;
  }
  if (schema.format === "uuid" && !uuidPattern.test(value)) {
    return `${location} must be a UUID`;
  }
  if (schema.format === "date-time" && !validDateTime(value)) {
    return `${location} must be an RFC 3339 UTC timestamp`;
  }
  if (
    schema.format !== undefined &&
    schema.format !== "uuid" &&
    schema.format !== "date-time"
  ) {
    return `${location} uses unsupported string format ${schema.format}`;
  }
  return undefined;
};

const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const validDateTime = (value: string): boolean => {
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/.exec(
      value,
    );
  if (!match) return false;
  const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
  if (month! < 1 || month! > 12 || hour! > 23 || minute! > 59 || second! > 59) {
    return false;
  }
  const leap = year! % 4 === 0 && (year! % 100 !== 0 || year! % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return day! >= 1 && day! <= days[month! - 1]!;
};

export const decodeBrowserActivityEvent =
  decoder<BrowserActivityEvent>(ActivityEventSchema);
export const decodeBrowserProjectBoard =
  decoder<BrowserProjectBoard>(ProjectBoardSchema);
export const decodeBrowserProjectList =
  decoder<BrowserProjectList>(ProjectListSchema);
export const decodeBrowserTicketDetail =
  decoder<BrowserTicketDetail>(TicketDetailSchema);
export const decodeBrowserTicketList =
  decoder<BrowserTicketList>(TicketListSchema);
