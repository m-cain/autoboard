defmodule AutoboardWeb.EventsStreamTest do
  use Autoboard.DataCase, async: false

  import Plug.Test

  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias AutoboardWeb.EventsStream

  test "strictly parses Last-Event-ID" do
    assert {:ok, 0} = EventsStream.parse_last_event_id([])
    assert {:ok, 42} = EventsStream.parse_last_event_id(["42"])
    assert {:error, :invalid_last_event_id} = EventsStream.parse_last_event_id(["-1"])
    assert {:error, :invalid_last_event_id} = EventsStream.parse_last_event_id(["2.5"])
    assert {:error, :invalid_last_event_id} = EventsStream.parse_last_event_id(["1", "2"])
  end

  test "formats the complete ActivityEvent payload used by the browser contract" do
    event = %{
      id: 8,
      event_type: "ticket.updated",
      actor: :codex,
      project_id: "project",
      ticket_id: nil,
      payload: %{"changed" => ["status"]},
      inserted_at: ~U[2026-07-17 12:00:00Z]
    }

    assert "id: 8\nevent: activity\ndata: {\"actor\":\"codex\",\"event_type\":\"ticket.updated\",\"id\":8,\"inserted_at\":\"2026-07-17T12:00:00Z\",\"payload\":{\"changed\":[\"status\"]},\"project_id\":\"project\",\"ticket_id\":null}\n\n" =
             EventsStream.format_event(event)
  end

  test "rejects a future Last-Event-ID as JSON before opening a stream" do
    conn = conn(:get, "/api/v1/events") |> Plug.Conn.put_req_header("last-event-id", "1")
    response = EventsStream.stream(conn)
    assert response.status == 400

    assert ["application/json; charset=utf-8"] =
             Plug.Conn.get_resp_header(response, "content-type")

    assert %{"error" => %{"kind" => "validation_failed"}} = Jason.decode!(response.resp_body)
  end

  test "streams bounded replay pages in activity-id order and cleans up on chunk failure" do
    {:ok, project} =
      Projects.create(Context.global(:codex), %{key: "SSE", name: "SSE", description: ""})

    baseline = Repo.aggregate(Event, :max, :id)
    events = Enum.map(1..5, &event_fixture(&1, project.id))
    parent = self()

    stream_in_child(fn ->
      EventsStream.stream(
        conn(:get, "/api/v1/events")
        |> Plug.Conn.put_req_header("last-event-id", to_string(baseline)),
        page_size: 2,
        chunker: fn current, data ->
          send(parent, {:sse_chunk, data})

          if String.starts_with?(data, "id: #{List.last(events).id}\n"),
            do: {:error, :closed},
            else: {:ok, current}
        end
      )
    end)

    ids =
      for _ <- events do
        assert_receive {:sse_chunk, "id: " <> rest}, 500
        [id | _] = String.split(rest, "\n", parts: 2)
        String.to_integer(id)
      end

    assert ids == Enum.map(events, & &1.id)
    assert [] == Registry.lookup(Autoboard.Activity.Registry, :activity)
  end

  test "schedules the true fifteen-second heartbeat and exits cleanly when heartbeat chunking fails" do
    parent = self()

    stream_in_child(fn ->
      EventsStream.stream(conn(:get, "/api/v1/events"),
        scheduler: fn pid, message, milliseconds ->
          send(parent, {:heartbeat_scheduled, milliseconds})
          Process.send_after(pid, message, 1)
        end,
        chunker: fn _current, data ->
          send(parent, {:sse_chunk, data})
          {:error, :closed}
        end
      )
    end)

    assert_receive {:heartbeat_scheduled, 15_000}, 500
    assert_receive {:sse_chunk, ": heartbeat\n\n"}, 500
    assert [] == Registry.lookup(Autoboard.Activity.Registry, :activity)
  end

  test "treats notifications as wakeups and drains committed rows in ascending order" do
    {:ok, project} =
      Projects.create(Context.global(:codex), %{key: "LIV", name: "Live", description: ""})

    baseline = Repo.aggregate(Event, :max, :id)
    parent = self()

    pid =
      start_stream_child(fn ->
        EventsStream.stream(
          conn(:get, "/api/v1/events")
          |> Plug.Conn.put_req_header("last-event-id", to_string(baseline)),
          chunker: fn current, data ->
            send(parent, {:sse_chunk, data})

            if String.contains?(data, "fixture.2") do
              {:error, :closed}
            else
              {:ok, current}
            end
          end
        )
      end)

    assert_eventually_registered(pid)
    first = event_fixture(1, project.id)
    second = event_fixture(2, project.id)
    first_prefix = "id: #{first.id}\n"
    second_prefix = "id: #{second.id}\n"
    send(pid, {:activity, %{id: second.id + 100}})
    send(pid, {:activity, %{id: first.id - 1}})

    assert_receive {:sse_chunk, ^first_prefix <> _}, 500
    assert_receive {:sse_chunk, ^second_prefix <> _}, 500
    assert_receive {:stream_finished, ^pid, _result}, 1_000
    assert [] == Registry.lookup(Autoboard.Activity.Registry, :activity)
  end

  defp event_fixture(number, project_id) do
    Repo.insert!(%Event{
      actor: :codex,
      event_type: "fixture.#{number}",
      project_id: project_id,
      payload: %{"number" => number}
    })
  end

  defp stream_in_child(fun) do
    pid = start_stream_child(fun)
    assert_receive {:stream_finished, ^pid, _result}, 1_000
  end

  defp start_stream_child(fun) do
    parent = self()

    pid =
      spawn(fn ->
        receive do
          :start ->
            result = fun.()
            send(parent, {:stream_finished, self(), result})
        end
      end)

    :ok = Ecto.Adapters.SQL.Sandbox.allow(Repo, parent, pid)
    send(pid, :start)
    pid
  end

  defp assert_eventually_registered(pid) do
    if Enum.any?(Registry.lookup(Autoboard.Activity.Registry, :activity), fn {registered, _value} ->
         registered == pid
       end) do
      :ok
    else
      Process.sleep(5)
      assert_eventually_registered(pid)
    end
  end
end
