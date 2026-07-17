import { Context, Data, Effect, Schema } from "effect"
import { Project, ProjectBoard, TicketDetail, TicketSummary, exactStruct } from "@autoboard/contracts"

const ProjectsResponse = exactStruct({ active: Schema.Array(Project), archived: Schema.Array(Project) })
const TicketList = exactStruct({ tickets: Schema.Array(TicketSummary) })

export class HttpError extends Data.TaggedError("HttpError")<{ readonly status: number; readonly message: string }> {}
export class NetworkError extends Data.TaggedError("NetworkError")<{ readonly message: string }> {}
export class DecodeError extends Data.TaggedError("DecodeError")<{ readonly message: string }> {}
export class RequestAbortedError extends Data.TaggedError("RequestAbortedError")<{ readonly message: string }> {}

export type ApiClientService = {
  readonly listProjects: (signal?: AbortSignal) => Effect.Effect<Schema.Schema.Type<typeof ProjectsResponse>, HttpError | NetworkError | DecodeError | RequestAbortedError>
  readonly listTriage: (signal?: AbortSignal) => Effect.Effect<Schema.Schema.Type<typeof TicketList>, HttpError | NetworkError | DecodeError | RequestAbortedError>
  readonly getProjectBoard: (key: string, signal?: AbortSignal) => Effect.Effect<Schema.Schema.Type<typeof ProjectBoard>, HttpError | NetworkError | DecodeError | RequestAbortedError>
  readonly getCanceledTickets: (key: string, signal?: AbortSignal) => Effect.Effect<Schema.Schema.Type<typeof TicketList>, HttpError | NetworkError | DecodeError | RequestAbortedError>
  readonly getTicket: (identifier: string, signal?: AbortSignal) => Effect.Effect<Schema.Schema.Type<typeof TicketDetail>, HttpError | NetworkError | DecodeError | RequestAbortedError>
}

export const ApiClient = Context.GenericTag<ApiClientService>("autoboard/ApiClient")

type Fetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
export type ApiClientOptions = { readonly fetch?: Fetch; readonly sleep?: (milliseconds: number, signal?: AbortSignal) => Promise<void> }

const isNetworkFailure = (error: unknown): boolean => error instanceof TypeError
const isAbortFailure = (error: unknown): boolean => error instanceof RequestAbortedError || (error instanceof DOMException && error.name === "AbortError")
const encodeSegment = (value: string) => encodeURIComponent(value)
const aborted = () => new RequestAbortedError({ message: "Request was aborted" })

const awaitWithAbort = <A>(promise: Promise<A>, signal?: AbortSignal): Promise<A> => {
  if (!signal) return promise
  if (signal.aborted) return Promise.reject(aborted())
  return new Promise<A>((resolve, reject) => {
    const onAbort = () => reject(aborted())
    signal.addEventListener("abort", onAbort, { once: true })
    void promise.then(
      (value) => { signal.removeEventListener("abort", onAbort); resolve(value) },
      (error: unknown) => { signal.removeEventListener("abort", onAbort); reject(error) },
    )
  })
}

export const createApiClient = (options: ApiClientOptions = {}): ApiClientService => {
  const fetcher = options.fetch ?? window.fetch.bind(window)
  const sleep = options.sleep ?? ((milliseconds: number, signal?: AbortSignal) => awaitWithAbort(new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds)), signal))

  const get = <S extends Schema.Schema.Any>(path: string, schema: S, signal?: AbortSignal): Effect.Effect<Schema.Schema.Type<S>, HttpError | NetworkError | DecodeError | RequestAbortedError> =>
    Effect.tryPromise({
      try: async () => {
        let retry = 0
        while (true) {
          try {
            if (signal?.aborted) throw aborted()
            const response = await awaitWithAbort(fetcher(path, { method: "GET", headers: { accept: "application/json" }, signal }), signal)
            if (!response.ok) {
              if (response.status === 503 && retry < 2) {
                await awaitWithAbort(sleep(retry === 0 ? 250 : 1000, signal), signal)
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
            if (isAbortFailure(error) || signal?.aborted) throw aborted()
            if (isNetworkFailure(error) && retry < 2) {
              await awaitWithAbort(sleep(retry === 0 ? 250 : 1000, signal), signal)
              retry += 1
              continue
            }
            throw error
          }
        }
      },
      catch: (error) => {
        if (error instanceof HttpError || error instanceof DecodeError || error instanceof RequestAbortedError) return error
        return new NetworkError({ message: error instanceof Error ? error.message : "Network request failed" })
      },
    })

  return {
    listProjects: (signal) => get("/api/v1/projects", ProjectsResponse, signal),
    listTriage: (signal) => get("/api/v1/triage", TicketList, signal),
    getProjectBoard: (key, signal) => get(`/api/v1/projects/${encodeSegment(key)}/board`, ProjectBoard, signal),
    getCanceledTickets: (key, signal) => get(`/api/v1/projects/${encodeSegment(key)}/canceled`, TicketList, signal),
    getTicket: (identifier, signal) => get(`/api/v1/tickets/${encodeSegment(identifier)}`, TicketDetail, signal),
  }
}
