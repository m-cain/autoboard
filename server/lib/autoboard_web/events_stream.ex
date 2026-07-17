defmodule AutoboardWeb.EventsStream do
  @moduledoc false

  import Plug.Conn

  alias Autoboard.Activity
  alias Autoboard.Auth.Context

  @heartbeat_ms 15_000
  @max_mailbox 1_000

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
        validation_response(conn, "Last-Event-ID must be one non-negative integer")

      {:error, _reason} ->
        unavailable_response(conn)
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

  defp stream_subscribed(conn, last_id, options) do
    with {:ok, high_water} <- Activity.high_water(context()),
         :ok <- valid_cursor(last_id, high_water) do
      conn = send_stream_headers(conn)

      case drain_until(conn, last_id, high_water, options) do
        {:ok, streamed, cursor} -> receive_events(streamed, cursor, options)
        {:error, streamed, _cursor} -> streamed
      end
    else
      {:error, :future_last_event_id} ->
        validation_response(conn, "Last-Event-ID is newer than the activity log")

      {:error, _reason} ->
        unavailable_response(conn)
    end
  end

  defp valid_cursor(last_id, high_water) when last_id <= high_water, do: :ok
  defp valid_cursor(_last_id, _high_water), do: {:error, :future_last_event_id}

  defp send_stream_headers(conn) do
    conn
    |> put_resp_content_type("text/event-stream")
    |> put_resp_header("cache-control", "no-cache")
    |> put_resp_header("connection", "keep-alive")
    |> send_chunked(200)
  end

  # The DB, not raw broadcasts, is the source of streamed events. Bounded pages
  # make a slow/reconnecting client unable to turn a large replay into one huge
  # in-memory response.
  defp drain_until(conn, cursor, upper, options) do
    case Activity.replay_page(context(), cursor, upper, page_size(options)) do
      {:ok, []} ->
        {:ok, conn, cursor}

      {:ok, events} ->
        case send_events(conn, events, options) do
          {:ok, streamed, latest} -> drain_until(streamed, latest, upper, options)
          {:error, streamed, latest} -> {:error, streamed, latest}
        end

      {:error, _reason} ->
        {:error, conn, cursor}
    end
  end

  defp send_events(conn, events, options) do
    Enum.reduce_while(events, {:ok, conn, nil}, fn event, {:ok, current, _latest} ->
      case chunk(current, format_event(event), options) do
        {:ok, streamed} -> {:cont, {:ok, streamed, event.id}}
        {:error, streamed} -> {:halt, {:error, streamed, event.id - 1}}
      end
    end)
  end

  defp receive_events(conn, cursor, options) do
    timer = schedule_heartbeat(options)

    try do
      receive do
        {:activity, _event} ->
          if overloaded?(options) do
            conn
          else
            drain_notifications()

            case drain_until(conn, cursor, nil, options) do
              {:ok, streamed, latest} -> receive_events(streamed, latest, options)
              {:error, streamed, _latest} -> streamed
            end
          end

        :autoboard_sse_heartbeat ->
          case chunk(conn, ": heartbeat\n\n", options) do
            {:ok, streamed} -> receive_events(streamed, cursor, options)
            {:error, streamed} -> streamed
          end
      end
    after
      cancel_heartbeat(timer, options)
    end
  end

  defp drain_notifications do
    receive do
      {:activity, _event} -> drain_notifications()
    after
      0 -> :ok
    end
  end

  defp chunk(conn, data, options) do
    case Keyword.get(options, :chunker, &Plug.Conn.chunk/2).(conn, data) do
      {:ok, streamed} -> {:ok, streamed}
      {:error, _reason} -> {:error, conn}
    end
  end

  defp page_size(options), do: Keyword.get(options, :page_size, Activity.replay_page_size())

  defp overloaded?(options) do
    max_mailbox = Keyword.get(options, :max_mailbox, @max_mailbox)
    {:message_queue_len, size} = Process.info(self(), :message_queue_len)
    size > max_mailbox
  end

  defp schedule_heartbeat(options) do
    Keyword.get(options, :scheduler, &Process.send_after/3).(
      self(),
      :autoboard_sse_heartbeat,
      @heartbeat_ms
    )
  end

  defp cancel_heartbeat(timer, options) do
    Keyword.get(options, :canceller, &Process.cancel_timer/1).(timer)
    :ok
  end

  defp validation_response(conn, message) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(
      400,
      Jason.encode!(%{
        "error" => %{
          "kind" => "validation_failed",
          "message" => message,
          "fields" => %{"last_event_id" => [message]},
          "current" => nil
        }
      })
    )
  end

  defp unavailable_response(conn), do: send_resp(conn, 503, "event stream unavailable")
  defp context, do: Context.global(:me)
  defp format_timestamp(%DateTime{} = timestamp), do: DateTime.to_iso8601(timestamp)
  defp format_timestamp(timestamp), do: timestamp
end
