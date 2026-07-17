import { Effect, Schema, Stream } from "effect"
import { ActivityEvent, type ActivityEvent as ActivityEventType } from "@autoboard/contracts"

export type EventSourceLike = {
  readonly addEventListener: (type: string, listener: (event: MessageEvent<string>) => void) => void
  readonly removeEventListener: (type: string, listener: (event: MessageEvent<string>) => void) => void
  onerror: (() => void) | null
  close: () => void
}

type Timer = ReturnType<typeof setTimeout>
export type ActivityStreamOptions = {
  readonly createEventSource?: (url: string) => EventSourceLike
  readonly schedule?: (reconnect: () => void, delay: number) => Timer
  readonly cancel?: (timer: Timer) => void
  readonly endpoint?: string
  readonly lastEventId?: number
}

const decodeActivity = Schema.decodeUnknownSync(ActivityEvent as unknown as Schema.Schema<ActivityEventType, unknown, never>)
const eventUrl = (endpoint: string, lastEventId: number) => `${endpoint}${endpoint.includes("?") ? "&" : "?"}last_event_id=${encodeURIComponent(String(lastEventId))}`

export const activityStream = (options: ActivityStreamOptions = {}): Stream.Stream<ActivityEventType> => Stream.async((emit) => {
  const createEventSource = options.createEventSource ?? ((url: string) => new EventSource(url) as unknown as EventSourceLike)
  const schedule = options.schedule ?? ((reconnect, delay) => window.setTimeout(reconnect, delay))
  const cancel = options.cancel ?? ((timer) => window.clearTimeout(timer))
  const endpoint = options.endpoint ?? "/api/v1/events"
  let active: EventSourceLike | undefined
  let activeActivity: ((event: MessageEvent<string>) => void) | undefined
  let reconnectTimer: Timer | undefined
  let closed = false
  let attempts = 0
  let greatestId = options.lastEventId ?? 0

  const retire = (source: EventSourceLike, onActivity: (event: MessageEvent<string>) => void) => {
    source.removeEventListener("activity", onActivity)
    source.onerror = null
    source.close()
  }

  const connect = () => {
    if (closed || active) return
    const source = createEventSource(eventUrl(endpoint, greatestId))
    active = source
    const onActivity = (message: MessageEvent<string>) => {
      if (closed || active !== source) return
      try {
        const decoded = decodeActivity(JSON.parse(message.data))
        const reportedId = message.lastEventId === "" ? undefined : Number(message.lastEventId)
        if ((reportedId !== undefined && (!Number.isSafeInteger(reportedId) || reportedId !== decoded.id)) || decoded.id <= greatestId) return
        greatestId = decoded.id
        attempts = 0
        void emit.single(decoded)
      } catch {
        // Invalid replay data is isolated; the next valid server event still invalidates the view.
      }
    }
    const reconnect = () => {
      if (closed || active !== source || reconnectTimer !== undefined) return
      retire(source, onActivity)
      active = undefined
      activeActivity = undefined
      const delay = Math.min(250 * (2 ** attempts), 10_000)
      attempts += 1
      let timer: Timer
      timer = schedule(() => {
        if (closed || reconnectTimer !== timer) return
        reconnectTimer = undefined
        connect()
      }, delay)
      reconnectTimer = timer
    }
    source.addEventListener("activity", onActivity)
    activeActivity = onActivity
    source.onerror = reconnect
  }

  connect()
  return Effect.sync(() => {
    closed = true
    if (reconnectTimer !== undefined) cancel(reconnectTimer)
    reconnectTimer = undefined
    if (active && activeActivity) retire(active, activeActivity)
    active = undefined
    activeActivity = undefined
  })
})
