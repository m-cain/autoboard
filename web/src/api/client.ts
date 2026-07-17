import { Context, Data, Effect, Schema } from "effect"
import { Project, ProjectBoard, TicketDetail, TicketSummary, exactStruct } from "@autoboard/contracts"

const ProjectsResponse = exactStruct({ active: Schema.Array(Project), archived: Schema.Array(Project) })
const TicketList = exactStruct({ tickets: Schema.Array(TicketSummary) })

export class HttpError extends Data.TaggedError("HttpError")<{ readonly status: number; readonly message: string }> {}
export class NetworkError extends Data.TaggedError("NetworkError")<{ readonly message: string }> {}
export class DecodeError extends Data.TaggedError("DecodeError")<{ readonly message: string }> {}

export type ApiClientService = {
  readonly listProjects: () => Effect.Effect<Schema.Schema.Type<typeof ProjectsResponse>, HttpError | NetworkError | DecodeError>
  readonly listTriage: () => Effect.Effect<Schema.Schema.Type<typeof TicketList>, HttpError | NetworkError | DecodeError>
  readonly getProjectBoard: (key: string) => Effect.Effect<Schema.Schema.Type<typeof ProjectBoard>, HttpError | NetworkError | DecodeError>
  readonly getCanceledTickets: (key: string) => Effect.Effect<Schema.Schema.Type<typeof TicketList>, HttpError | NetworkError | DecodeError>
  readonly getTicket: (identifier: string) => Effect.Effect<Schema.Schema.Type<typeof TicketDetail>, HttpError | NetworkError | DecodeError>
}

export const ApiClient = Context.GenericTag<ApiClientService>("autoboard/ApiClient")

type Fetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
export type ApiClientOptions = { readonly fetch?: Fetch; readonly sleep?: (milliseconds: number) => Promise<void> }

const isNetworkFailure = (error: unknown): boolean => error instanceof TypeError
const encodeSegment = (value: string) => encodeURIComponent(value)

export const createApiClient = (options: ApiClientOptions = {}): ApiClientService => {
  const fetcher = options.fetch ?? window.fetch.bind(window)
  const sleep = options.sleep ?? ((milliseconds: number) => new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds)))

  const get = <S extends Schema.Schema.Any>(path: string, schema: S): Effect.Effect<Schema.Schema.Type<S>, HttpError | NetworkError | DecodeError> =>
    Effect.tryPromise({
      try: async () => {
        let retry = 0
        while (true) {
          try {
            const response = await fetcher(path, { method: "GET", headers: { accept: "application/json" } })
            if (!response.ok) {
              if (response.status === 503 && retry < 2) {
                await sleep(retry === 0 ? 250 : 1000)
                retry += 1
                continue
              }
              throw new HttpError({ status: response.status, message: response.statusText || `HTTP ${response.status}` })
            }

            let payload: unknown
            try {
              payload = await response.json()
            } catch {
              throw new DecodeError({ message: "Response was not valid JSON" })
            }

            try {
              // All browser transport schemas are context-free. `exactStruct`
              // currently retains `unknown` in its public context parameter, so
              // establish that boundary once rather than accepting unknown JSON.
              return await Schema.decodeUnknownPromise(schema as unknown as Schema.Schema<Schema.Schema.Type<S>, Schema.Schema.Encoded<S>, never>)(payload)
            } catch {
              throw new DecodeError({ message: "Response did not match the expected schema" })
            }
          } catch (error) {
            if (isNetworkFailure(error) && retry < 2) {
              await sleep(retry === 0 ? 250 : 1000)
              retry += 1
              continue
            }
            throw error
          }
        }
      },
      catch: (error) => {
        if (error instanceof HttpError || error instanceof DecodeError) return error
        return new NetworkError({ message: error instanceof Error ? error.message : "Network request failed" })
      },
    })

  return {
    listProjects: () => get("/api/v1/projects", ProjectsResponse),
    listTriage: () => get("/api/v1/triage", TicketList),
    getProjectBoard: (key) => get(`/api/v1/projects/${encodeSegment(key)}/board`, ProjectBoard),
    getCanceledTickets: (key) => get(`/api/v1/projects/${encodeSegment(key)}/canceled`, TicketList),
    getTicket: (identifier) => get(`/api/v1/tickets/${encodeSegment(identifier)}`, TicketDetail),
  }
}
