defmodule AutoboardWeb.EventsStream do
  @moduledoc false

  import Plug.Conn

  alias Autoboard.Activity
  alias Autoboard.Auth.Context

  @heartbeat_ms 15_000

  @spec stream(Plug.Conn.t(), keyword()) :: Plug.Conn.t()
  def stream(conn, options \\ []) do
    with {:ok, last_id} <- parse_last_event_id(get_req_header(conn, "last-event-id")),
         :ok <- Activity.subscribe() do
      try do
        stream_subscribed(conn, last_id, options)
      after
        Activity.unsubscribe()
      end
    else
      {:error, :invalid_last_event_id} ->
        conn
        |> put_resp_content_type("application/json")
        |> send_resp(
          400,
          Jason.encode!(%{
            "error" => %{
              "kind" => "validation_failed",
              "message" => "Last-Event-ID must be one non-negative integer",
              "fields" => %{"last_event_id" => ["must be a non-negative integer"]},
              "current" => nil
            }
          })
        )

      {:error, _reason} ->
        send_resp(conn, 503, "event stream unavailable")
    end
  end

  defp stream_subscribed(conn, last_id, options) do
    with {:ok, high_water} <- Activity.high_water(Context.global(:me)),
         {:ok, replay} <- Activity.replay_between(Context.global(:me), last_id, high_water),
         {:ok, conn} <- send_stream_headers(conn) do
      case send_events(conn, replay, options) do
        {:ok, streamed} ->
          receive_events_with_heartbeat(streamed, max(last_id, high_water), options)

        {:error, streamed} ->
          streamed
      end
    else
      {:error, _reason} -> send_resp(conn, 503, "event stream unavailable")
    end
  end

  @spec parse_last_event_id([String.t()]) ::
          {:ok, non_neg_integer()} | {:error, :invalid_last_event_id}
  def parse_last_event_id([]), do: {:ok, 0}

  def parse_last_event_id([value]) when is_binary(value) do
    case Integer.parse(value) do
      {id, ""} when id >= 0 -> {:ok, id}
      _ -> {:error, :invalid_last_event_id}
    end
  end

  def parse_last_event_id(_), do: {:error, :invalid_last_event_id}

  @spec format_event(map() | struct()) :: String.t()
  def format_event(event) do
    payload = %{
      "event_type" => event.event_type,
      "project_id" => event.project_id,
      "ticket_id" => event.ticket_id,
      "inserted_at" => format_timestamp(event.inserted_at)
    }

    "id: #{event.id}\nevent: activity\ndata: #{Jason.encode!(payload)}\n\n"
  end

  @spec select_live(non_neg_integer(), [map() | struct()]) :: {non_neg_integer(), [pos_integer()]}
  def select_live(last_id, events) do
    Enum.reduce(events, {last_id, []}, fn event, {current, selected} ->
      if is_integer(event.id) and event.id > current do
        {event.id, selected ++ [event.id]}
      else
        {current, selected}
      end
    end)
  end

  defp send_stream_headers(conn) do
    conn
    |> put_resp_content_type("text/event-stream")
    |> put_resp_header("cache-control", "no-cache")
    |> put_resp_header("connection", "keep-alive")
    |> send_chunked(200)
  end

  defp send_events(conn, events, options) do
    Enum.reduce_while(events, {:ok, conn}, fn event, {:ok, acc} ->
      case chunk(acc, format_event(event), options) do
        {:ok, updated} -> {:cont, {:ok, updated}}
        {:error, updated} -> {:halt, {:error, updated}}
      end
    end)
  end

  defp receive_events(conn, last_id, timer, options) do
    receive do
      {:activity, event} when is_integer(event.id) and event.id > last_id ->
        case chunk(conn, format_event(event), options) do
          {:ok, updated} -> receive_events(updated, event.id, timer, options)
          {:error, updated} -> updated
        end

      {:activity, _event} ->
        receive_events(conn, last_id, timer, options)

      :autoboard_sse_heartbeat ->
        case chunk(conn, ": heartbeat\n\n", options) do
          {:ok, updated} ->
            next =
              Process.send_after(
                self(),
                :autoboard_sse_heartbeat,
                Keyword.get(options, :heartbeat_ms, @heartbeat_ms)
              )

            Process.cancel_timer(timer)
            receive_events(updated, last_id, next, options)

          {:error, updated} ->
            updated
        end
    end
  end

  defp receive_events_with_heartbeat(conn, last_id, options) do
    timer =
      Process.send_after(
        self(),
        :autoboard_sse_heartbeat,
        Keyword.get(options, :heartbeat_ms, @heartbeat_ms)
      )

    try do
      receive_events(conn, last_id, timer, options)
    after
      Process.cancel_timer(timer)
    end
  end

  defp chunk(conn, data, options) do
    case Keyword.get(options, :chunker, &Plug.Conn.chunk/2).(conn, data) do
      {:ok, updated} -> {:ok, updated}
      {:error, updated} -> {:error, updated}
    end
  end

  defp format_timestamp(%DateTime{} = timestamp), do: DateTime.to_iso8601(timestamp)
  defp format_timestamp(timestamp), do: timestamp
end
