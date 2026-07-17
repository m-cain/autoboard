defmodule Autoboard.ReadModelTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Attachments
  alias Autoboard.Auth.Context
  alias Autoboard.Comments
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.ReadModel
  alias Autoboard.Tickets

  setup do
    ctx = Context.global(:codex)
    project = project_fixture(ctx, "AUTO")
    fixture = complete_fixture(ctx, project)
    %{ctx: ctx, project: project, fixture: fixture}
  end

  test "keeps board, triage, and canceled tickets in distinct deterministic reads", %{
    ctx: ctx,
    project: project,
    fixture: fixture
  } do
    assert {:ok, board} = ReadModel.project_board(ctx, project.key)
    assert Map.keys(board.columns) |> Enum.sort() == ["backlog", "done", "in_progress", "ready"]
    assert Enum.map(board.columns["backlog"], & &1.id) == [fixture.parent.id]
    assert Enum.map(board.columns["ready"], & &1.id) == [fixture.blocked.id]
    assert Enum.map(board.columns["in_progress"], & &1.id) == [fixture.blocker.id]
    assert Enum.map(board.columns["done"], & &1.id) == [fixture.child.id, fixture.done.id]

    assert {:ok, triage} = ReadModel.triage_tickets(ctx)
    assert Enum.map(triage, & &1.id) == [fixture.triage.id]

    assert {:ok, canceled} = ReadModel.canceled_tickets(ctx, project.key)
    assert Enum.map(canceled, & &1.id) == [fixture.canceled.id]
  end

  test "returns a fully aggregated ticket detail with a bounded newest-first activity list", %{
    ctx: ctx,
    fixture: fixture
  } do
    assert {:ok, detail} = ReadModel.ticket_detail(ctx, fixture.blocked.identifier)

    assert detail.ticket.id == fixture.blocked.id
    assert detail.ticket.identifier == fixture.blocked.identifier
    assert detail.ticket.revision > fixture.blocked.revision
    assert detail.blocked
    assert Enum.map(detail.labels, & &1.name) == ["backend", "Bug"]
    assert detail.parent == nil
    assert detail.subtasks == []
    assert Enum.map(detail.blockers, & &1.id) == [fixture.blocker.id]
    assert detail.blocked_tickets == []

    assert [%{id: comment_id, actor: :codex, body: "Investigating the dependency"}] =
             detail.comments

    assert comment_id == fixture.comment.id

    assert [%{id: attachment_id, original_filename: "evidence.txt", managed_path: managed_path}] =
             detail.attachments

    assert attachment_id == fixture.attachment.id
    assert managed_path == fixture.attachment.managed_path

    assert Enum.map(detail.activity, & &1.id) ==
             Enum.sort(Enum.map(detail.activity, & &1.id), :desc)

    assert length(detail.activity) <= 100
  end

  test "loads ticket detail in a fixed query count rather than per relationship row", %{
    ctx: ctx,
    fixture: fixture
  } do
    query_count =
      count_queries(fn ->
        assert {:ok, _detail} = ReadModel.ticket_detail(ctx, fixture.blocked.id)
      end)

    assert query_count == 9
  end

  test "searches title and description case-insensitively, caps results, and delegates actionable rules",
       %{
         ctx: ctx,
         project: project,
         fixture: fixture
       } do
    assert {:ok, [match]} =
             ReadModel.search_tickets(ctx, %{query: "DEPENDENCY", project_id: project.id})

    assert match.id == fixture.blocked.id

    assert {:ok, actionable} =
             ReadModel.actionable_tickets(ctx, %{project_id: project.id, limit: 100})

    assert actionable == []

    assert {:error, %Error{kind: :validation_failed, fields: %{limit: [_]}}} =
             ReadModel.search_tickets(ctx, %{query: "x", limit: 101})
  end

  test "requires a global authorization context before reads", %{project: project} do
    invalid_ctx = %Context{actor: :system, scope: :global}

    assert {:error, %Error{kind: :unauthorized}} = ReadModel.list_projects(invalid_ctx)

    assert {:error, %Error{kind: :unauthorized}} =
             ReadModel.project_board(invalid_ctx, project.key)
  end

  defp complete_fixture(ctx, project) do
    triage = ticket_fixture(ctx, project, %{title: "Triage"})
    parent = ticket_fixture(ctx, project, %{title: "Parent", status: :backlog})

    child =
      ticket_fixture(ctx, project, %{title: "Child", status: :done, parent_ticket_id: parent.id})

    blocker = ticket_fixture(ctx, project, %{title: "Blocker", status: :in_progress})

    blocked =
      ticket_fixture(ctx, project, %{
        title: "Search dependency",
        description: "Needs a BACKEND dependency before release",
        status: :ready,
        assignee: :codex,
        labels: ["Bug", "backend"]
      })

    done = ticket_fixture(ctx, project, %{title: "Done", status: :done})
    canceled = ticket_fixture(ctx, project, %{title: "Canceled", status: :canceled})
    assert {:ok, _updated} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)
    assert {:ok, comment} = Comments.add(ctx, blocked.id, %{body: "Investigating the dependency"})
    attachment = attachment_fixture(ctx, blocked)

    %{
      triage: triage,
      parent: parent,
      child: child,
      blocker: blocker,
      blocked: blocked,
      done: done,
      canceled: canceled,
      comment: comment,
      attachment: attachment
    }
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} =
             Projects.create(ctx, %{key: key, name: "#{key} project", description: ""})

    project
  end

  defp ticket_fixture(ctx, project, attrs) do
    defaults = %{project_id: project.id, title: "Ticket #{System.unique_integer([:positive])}"}
    assert {:ok, ticket} = Tickets.create(ctx, Map.merge(defaults, attrs))
    ticket
  end

  defp attachment_fixture(ctx, ticket) do
    temp_dir = Path.join(System.tmp_dir!(), "autoboard-read-model-#{Ecto.UUID.generate()}")
    source_path = Path.join(temp_dir, "evidence.txt")
    File.mkdir_p!(temp_dir)
    File.write!(source_path, "evidence")
    on_exit(fn -> File.rm_rf(temp_dir) end)
    assert {:ok, attachment} = Attachments.add_from_path(ctx, ticket.id, source_path)
    attachment
  end

  defp count_queries(fun) do
    handler_id = "read-model-query-count-#{System.unique_integer([:positive])}"
    parent = self()

    :ok =
      :telemetry.attach(
        handler_id,
        [:autoboard, :repo, :query],
        fn _event, _measurements, _metadata, _config ->
          send(parent, {:read_model_query})
        end,
        nil
      )

    try do
      fun.()
      drain_query_count(0)
    after
      :telemetry.detach(handler_id)
    end
  end

  defp drain_query_count(count) do
    receive do
      {:read_model_query} -> drain_query_count(count + 1)
    after
      0 -> count
    end
  end
end
