defmodule AutoboardWeb.EventsStreamTest do
  use ExUnit.Case, async: true

  alias AutoboardWeb.EventsStream

  test "strictly parses Last-Event-ID" do
    assert {:ok, 0} = EventsStream.parse_last_event_id([])
    assert {:ok, 42} = EventsStream.parse_last_event_id(["42"])
    assert {:error, :invalid_last_event_id} = EventsStream.parse_last_event_id(["-1"])
    assert {:error, :invalid_last_event_id} = EventsStream.parse_last_event_id(["2.5"])
    assert {:error, :invalid_last_event_id} = EventsStream.parse_last_event_id(["1", "2"])
  end

  test "formats the minimum activity invalidation payload" do
    event = %{
      id: 8,
      event_type: "ticket.updated",
      project_id: "project",
      ticket_id: nil,
      inserted_at: ~U[2026-07-17 12:00:00Z]
    }

    assert "id: 8\nevent: activity\ndata: {\"event_type\":\"ticket.updated\",\"inserted_at\":\"2026-07-17T12:00:00Z\",\"project_id\":\"project\",\"ticket_id\":null}\n\n" =
             EventsStream.format_event(event)
  end

  test "filters stale and duplicate live events after replay high water" do
    assert {10, [8, 10]} =
             EventsStream.select_live(7, [%{id: 6}, %{id: 8}, %{id: 8}, %{id: 10}, %{id: 9}])
  end
end
