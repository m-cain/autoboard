defmodule AutoboardWeb.EventsStream do
  @moduledoc false

  import Plug.Conn

  alias Autoboard.Activity
  alias Autoboard.Auth.Context

  @heartbeat_ms 15_000
  @max_mailbox 1_000

  @spec stream(Plug.Conn.t(), keyword()) :: Plug.Conn.t()
  def stream(conn, options \\ []) do
    with {:ok, last_id} <- parse_last_event_id(last_event_ids(conn)),
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
      "id" => event.id,
      "event_type" => event.event_type,
      "actor" => format_actor(Map.get(event, :actor, :system)),
      "project_id" => event.project_id,
      "ticket_id" => event.ticket_id,
      "payload" => Map.get(event, :payload, %{}),
      "inserted_at" => format_timestamp(event.inserted_at)
    }

    "id: #{event.id}\nevent: activity\ndata: #{Jason.encode!(payload)}\n\n"
  end

  defp stream_subscribed(conn, last_id, options) do
    with {:ok, high_water} <- Activity.high_water(context()),
         :ok <- valid_cursor(last_id, high_water) do
      conn = send_stream_headers(conn)

      case drain_until(conn, last_id, high_water, options) do
        {:ok, streamed, cursor} ->
          receive_events(streamed, cursor, schedule_heartbeat(options), options)

        {:error, streamed, _cursor} ->
          streamed
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
    if overloaded?(options) do
      {:error, conn, cursor}
    else
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
  end

  defp send_events(conn, events, options) do
    Enum.reduce_while(events, {:ok, conn, nil}, fn event, {:ok, current, _latest} ->
      case chunk(current, format_event(event), options) do
        {:ok, streamed} -> {:cont, {:ok, streamed, event.id}}
        {:error, streamed} -> {:halt, {:error, streamed, event.id - 1}}
      end
    end)
  end

  defp receive_events(conn, cursor, timer, options) do
    receive do
      {:activity, _event} ->
        case drain_live(conn, cursor, options) do
          {:ok, streamed, latest} -> receive_events(streamed, latest, timer, options)
          {:error, streamed, _latest} -> cancel_heartbeat(timer, options) && streamed
        end

      :autoboard_sse_heartbeat ->
        case chunk(conn, ": heartbeat\n\n", options) do
          {:ok, streamed} ->
            cancel_heartbeat(timer, options)
            receive_events(streamed, cursor, schedule_heartbeat(options), options)

          {:error, streamed} ->
            cancel_heartbeat(timer, options)
            streamed
        end
    end
  end

  defp drain_live(conn, cursor, options) do
    with {:ok, high_water} <- Activity.high_water(context()),
         {:ok, streamed, latest} <- drain_until(conn, cursor, high_water, options) do
      if drain_notifications() do
        drain_live(streamed, latest, options)
      else
        {:ok, streamed, latest}
      end
    else
      {:error, streamed, latest} -> {:error, streamed, latest}
      _ -> {:error, conn, cursor}
    end
  end

  defp drain_notifications do
    receive do
      {:activity, _event} ->
        _ = drain_notifications()
        true
    after
      0 -> false
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
  defp format_actor(actor) when is_atom(actor), do: Atom.to_string(actor)
  defp format_actor(actor) when is_binary(actor), do: actor

  # Native EventSource automatically sends Last-Event-ID when it owns a retry.
  # A client-controlled exponential reconnect cannot set arbitrary headers, so
  # accept the equivalent query cursor only when the header is absent.
  defp last_event_ids(conn) do
    case get_req_header(conn, "last-event-id") do
      [] ->
        case conn
             |> fetch_query_params()
             |> Map.get(:query_params, %{})
             |> Map.get("last_event_id") do
          nil -> []
          value -> [value]
        end

      headers ->
        headers
    end
  end
end
