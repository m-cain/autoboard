defmodule Autoboard.PresenterTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Activity.Event
  alias Autoboard.Attachments.Attachment
  alias Autoboard.Comments.Comment
  alias Autoboard.Domain.Error
  alias Autoboard.Presenter
  alias Autoboard.Projects.Project
  alias Autoboard.Tickets.Label
  alias Autoboard.Tickets.Ticket

  defmodule UnsupportedCurrent do
    defstruct [:managed_path, :value]
  end

  @fixtures Path.expand("../fixtures/contracts", __DIR__)

  test "presents representative board and detail contracts without managed storage paths" do
    %{
      project: project,
      ticket: ticket,
      blocker: blocker,
      detail: detail,
      parent_detail: parent_detail
    } = contract_fixture()

    board_map =
      Presenter.board(project, %{
        "backlog" => [],
        "ready" => [ticket],
        "in_progress" => [blocker],
        "done" => []
      })

    detail_map = Presenter.ticket_detail(detail)

    refute contains_key?(detail_map, "managed_path")
    assert detail_map["blocked"] == true
    assert_fixture_equals("project_board.json", board_map)
    assert_fixture_equals("ticket_detail.json", detail_map)
    assert_fixture_equals("parent_ticket_detail.json", Presenter.ticket_detail(parent_detail))
  end

  test "normalizes non-UTC datetimes to UTC ISO8601" do
    %{project: project} = contract_fixture()
    {:ok, mountain_time, _offset} = DateTime.from_iso8601("2026-07-16T06:34:56.123456-06:00")

    assert Presenter.project(%{project | updated_at: mountain_time})["updated_at"] ==
             "2026-07-16T12:34:56.123456Z"
  end

  test "presents summary state and attachment paths only on explicit RPC requests" do
    %{ticket: ticket, attachment: attachment} = contract_fixture()

    assert %{"blocked" => true, "comment_count" => 1, "attachment_count" => 1} =
             Presenter.ticket_summary(ticket)

    refute Map.has_key?(Presenter.attachment(attachment), "managed_path")
    assert Presenter.attachment(attachment, true)["managed_path"] == attachment.managed_path
    assert_fixture_equals("attachment_rpc.json", Presenter.attachment(attachment, true))
  end

  test "presents structured errors with string keys and a presented current entity" do
    %{project: project} = contract_fixture()
    project_id = project.id

    error = %Error{
      kind: :revision_conflict,
      message: "project has changed",
      fields: %{name: ["changed"]},
      current: project
    }

    assert %{
             "kind" => "revision_conflict",
             "message" => "project has changed",
             "fields" => %{"name" => ["changed"]},
             "current" => %{"id" => ^project_id, "state" => "active"}
           } = Presenter.error(error)

    assert_fixture_equals("error_current.json", Presenter.error(error))
  end

  test "sanitizes supported and unsupported error currents without Ecto internals or managed paths" do
    %{ticket: ticket, attachment: attachment} = contract_fixture()
    ticket_id = ticket.id

    assert %{"current" => %{"id" => ^ticket_id, "identifier" => "AUTO-2"}} =
             Presenter.error(%Error{
               kind: :revision_conflict,
               message: "ticket changed",
               current: ticket
             })

    assert %{"current" => attachment_current} =
             Presenter.error(%Error{
               kind: :revision_conflict,
               message: "attachment changed",
               current: attachment
             })

    refute Map.has_key?(attachment_current, "managed_path")

    not_loaded = %Ticket{}.labels

    current = %{
      "attachment" => attachment,
      "not_loaded" => not_loaded,
      "unknown" => %UnsupportedCurrent{managed_path: "/private/secret", value: "ignore"}
    }

    presented = Presenter.error(%Error{kind: :internal_error, message: "safe", current: current})
    refute contains_key?(presented, "managed_path")
    refute contains_key?(presented, "__struct__")
    refute contains_key?(presented, "__meta__")
    assert presented["current"]["not_loaded"] == nil
  end

  defp assert_fixture_equals(filename, map) do
    path = Path.join(@fixtures, filename)
    assert {:ok, decoded} = path |> File.read!() |> Jason.decode()
    assert decoded == map
  end

  defp contains_key?(value, key) when is_map(value) do
    Map.has_key?(value, key) or
      Enum.any?(value, fn {_key, nested} -> contains_key?(nested, key) end)
  end

  defp contains_key?(value, key) when is_list(value),
    do: Enum.any?(value, &contains_key?(&1, key))

  defp contains_key?(_value, _key), do: false

  defp contract_fixture do
    timestamp = ~U[2026-07-16 12:34:56.123456Z]

    project = %Project{
      id: "11111111-1111-4111-8111-111111111111",
      key: "AUTO",
      name: "Autoboard",
      description: "Plan",
      state: :active,
      revision: 3,
      inserted_at: timestamp,
      updated_at: timestamp
    }

    label = %Label{
      id: "22222222-2222-4222-8222-222222222222",
      project_id: project.id,
      name: "Contract"
    }

    parent = %Ticket{
      id: "77777777-7777-4777-8777-777777777777",
      project_id: project.id,
      identifier: "AUTO-0",
      number: 0,
      title: "Parent ticket",
      description: "Contains the representative subtask",
      status: :backlog,
      priority: :none,
      assignee: :unassigned,
      revision: 2,
      blocked: false,
      comment_count: 0,
      attachment_count: 0,
      labels: [],
      inserted_at: timestamp,
      updated_at: timestamp
    }

    blocker = %Ticket{
      id: "33333333-3333-4333-8333-333333333333",
      project_id: project.id,
      identifier: "AUTO-1",
      number: 1,
      title: "Blocker",
      description: "",
      status: :in_progress,
      priority: :medium,
      assignee: :unassigned,
      revision: 1,
      blocked: false,
      comment_count: 0,
      attachment_count: 0,
      labels: [],
      inserted_at: timestamp,
      updated_at: timestamp
    }

    ticket = %Ticket{
      id: "44444444-4444-4444-8444-444444444444",
      project_id: project.id,
      identifier: "AUTO-2",
      number: 2,
      title: "Representative ticket",
      description: "A transport contract",
      status: :ready,
      priority: :high,
      assignee: :codex,
      revision: 4,
      parent_ticket_id: parent.id,
      blocked: true,
      comment_count: 1,
      attachment_count: 1,
      labels: [label],
      inserted_at: timestamp,
      updated_at: timestamp
    }

    comment = %Comment{
      id: "55555555-5555-4555-8555-555555555555",
      project_id: project.id,
      ticket_id: ticket.id,
      body: "A durable comment",
      actor: :codex,
      inserted_at: timestamp
    }

    attachment = %Attachment{
      id: "66666666-6666-4666-8666-666666666666",
      project_id: project.id,
      ticket_id: ticket.id,
      original_filename: "notes.txt",
      media_type: "text/plain",
      byte_size: 7,
      sha256: String.duplicate("a", 64),
      managed_path: "/private/autoboard/attachments/66666666-6666-4666-8666-666666666666",
      actor: :codex,
      inserted_at: timestamp
    }

    blocked_ticket = %Ticket{
      id: "88888888-8888-4888-8888-888888888888",
      project_id: project.id,
      identifier: "AUTO-3",
      number: 3,
      title: "Blocked ticket",
      description: "Waits on the representative ticket",
      status: :ready,
      priority: :low,
      assignee: :unassigned,
      revision: 1,
      blocked: true,
      comment_count: 0,
      attachment_count: 0,
      labels: [],
      inserted_at: timestamp,
      updated_at: timestamp
    }

    event = %Event{
      id: 42,
      event_type: "comment.added",
      actor: :codex,
      project_id: project.id,
      ticket_id: ticket.id,
      payload: %{"comment_id" => comment.id},
      inserted_at: timestamp
    }

    %{
      project: project,
      ticket: ticket,
      blocker: blocker,
      attachment: attachment,
      detail: %{
        project: project,
        ticket: ticket,
        labels: [label],
        parent: parent,
        subtasks: [],
        blockers: [blocker],
        blocked_tickets: [blocked_ticket],
        comments: [comment],
        attachments: [attachment],
        activity: [event],
        blocked: true
      },
      parent_detail: %{
        project: project,
        ticket: parent,
        labels: [],
        parent: nil,
        subtasks: [ticket],
        blockers: [],
        blocked_tickets: [],
        comments: [],
        attachments: [],
        activity: [],
        blocked: false
      }
    }
  end
end
