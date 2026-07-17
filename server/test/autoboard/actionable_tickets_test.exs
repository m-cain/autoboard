defmodule Autoboard.ActionableTicketsTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Tickets

  setup do
    ctx = Context.global(:me)
    %{ctx: ctx, project: project_fixture(ctx, "AUTO")}
  end

  test "Codex work excludes human, unassigned, blocked, and parent tickets", %{
    ctx: ctx,
    project: project
  } do
    codex = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex})
    _human = ticket_fixture(ctx, project, %{status: :ready, assignee: :me})
    _unassigned = ticket_fixture(ctx, project, %{status: :ready, assignee: :unassigned})
    parent = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex})
    _child = ticket_fixture(ctx, project, %{parent_ticket_id: parent.id})
    blocker = ticket_fixture(ctx, project)
    blocked = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex})
    assert {:ok, _blocked} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)

    assert Enum.map(Tickets.list_actionable(ctx, %{limit: 100}), & &1.id) == [codex.id]
  end

  test "actionable work permits terminal blockers and sorts urgent through none", %{
    ctx: ctx,
    project: project
  } do
    none = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex, priority: :none})
    high = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex, priority: :high})
    urgent = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex, priority: :urgent})
    blocker = ticket_fixture(ctx, project)

    resolved =
      ticket_fixture(ctx, project, %{status: :ready, assignee: :codex, priority: :medium})

    assert {:ok, _resolved} =
             Tickets.add_dependency(ctx, resolved.id, blocker.id, resolved.revision)

    assert {:ok, _blocker} = Tickets.transition(ctx, blocker.id, blocker.revision, :canceled)

    assert Enum.map(Tickets.list_actionable(ctx, %{limit: 100}), & &1.id) == [
             urgent.id,
             high.id,
             resolved.id,
             none.id
           ]
  end

  test "actionable work supports a bounded project filter", %{ctx: ctx, project: project} do
    current = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex})
    other_project = project_fixture(ctx, "OTHR")
    other = ticket_fixture(ctx, other_project, %{status: :ready, assignee: :codex})

    assert Enum.map(Tickets.list_actionable(ctx, %{project_id: project.id, limit: 1}), & &1.id) ==
             [
               current.id
             ]

    assert {:error, %Error{kind: :validation_failed, fields: %{limit: [_]}}} =
             Tickets.list_actionable(ctx, %{limit: 0})

    assert {:error, %Error{kind: :validation_failed, fields: %{project_id: [_]}}} =
             Tickets.list_actionable(ctx, %{project_id: "not-a-uuid", limit: 1})

    assert other.id != current.id
  end

  test "actionable work authorizes before validating filters", %{ctx: ctx} do
    assert {:error, %Error{kind: :unauthorized}} =
             Tickets.list_actionable(%Context{actor: :system, scope: :global}, %{limit: 0})

    assert Tickets.list_actionable(ctx, %{limit: 100}) == []
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} =
             Projects.create(ctx, %{key: key, name: "#{key} project", description: ""})

    project
  end

  defp ticket_fixture(ctx, project, attrs \\ %{}) do
    defaults = %{project_id: project.id, title: "Ticket #{System.unique_integer([:positive])}"}
    assert {:ok, ticket} = Tickets.create(ctx, Map.merge(defaults, attrs))
    ticket
  end
end
