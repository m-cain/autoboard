defmodule Autoboard.DependenciesTest do
  use Autoboard.DataCase, async: false

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias Autoboard.Tickets
  alias Autoboard.Tickets.Graph

  setup do
    %{ctx: Context.global(:me), project: project_fixture(Context.global(:me), "AUTO")}
  end

  test "graph reachability follows directed edges without recursion" do
    edges = [{"a", "b"}, {"b", "c"}, {"c", "d"}]

    assert Graph.reachable?(edges, "a", "d")
    assert Graph.reachable?(edges, "a", "a")
    refute Graph.reachable?(edges, "d", "a")
  end

  test "dependency mutations reject self edges, duplicates, and cross-project edges", %{
    ctx: ctx,
    project: project
  } do
    blocked = ticket_fixture(ctx, project)
    blocker = ticket_fixture(ctx, project)
    other_project = project_fixture(ctx, "OTHR")
    other = ticket_fixture(ctx, other_project)

    assert {:error, %Error{kind: :validation_failed, fields: %{blocker_ticket_id: [_]}}} =
             Tickets.add_dependency(ctx, blocked.id, blocked.id, blocked.revision)

    assert {:error, %Error{kind: :validation_failed, fields: %{blocker_ticket_id: [_]}}} =
             Tickets.add_dependency(ctx, blocked.id, other.id, blocked.revision)

    assert {:ok, updated} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)
    assert updated.revision == blocked.revision + 1

    assert {:error, %Error{kind: :validation_failed, fields: %{blocker_ticket_id: [_]}}} =
             Tickets.add_dependency(ctx, updated.id, blocker.id, updated.revision)

    assert Enum.take(event_types(project.id), 3) == [
             "dependency.added",
             "ticket.created",
             "ticket.created"
           ]
  end

  test "dependency mutations reject direct and multi-hop cycles", %{ctx: ctx, project: project} do
    first = ticket_fixture(ctx, project)
    second = ticket_fixture(ctx, project)
    third = ticket_fixture(ctx, project)

    assert {:ok, first} = Tickets.add_dependency(ctx, first.id, second.id, first.revision)

    assert {:error, %Error{kind: :dependency_cycle}} =
             Tickets.add_dependency(ctx, second.id, first.id, second.revision)

    assert {:ok, _second} = Tickets.add_dependency(ctx, second.id, third.id, second.revision)

    assert {:error, %Error{kind: :dependency_cycle}} =
             Tickets.add_dependency(ctx, third.id, first.id, third.revision)
  end

  test "remove requires an existing dependency and advances the blocked ticket revision", %{
    ctx: ctx,
    project: project
  } do
    blocked = ticket_fixture(ctx, project)
    blocker = ticket_fixture(ctx, project)

    assert {:error, %Error{kind: :validation_failed, fields: %{blocker_ticket_id: [_]}}} =
             Tickets.remove_dependency(ctx, blocked.id, blocker.id, blocked.revision)

    assert {:ok, blocked} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)

    assert {:ok, removed} =
             Tickets.remove_dependency(ctx, blocked.id, blocker.id, blocked.revision)

    assert removed.revision == blocked.revision + 1
    refute Tickets.blocked?(ctx, removed)
  end

  test "terminal blockers resolve blocking and transition changes notify directly blocked tickets once",
       %{
         ctx: ctx,
         project: project
       } do
    blocker = ticket_fixture(ctx, project)
    blocked = ticket_fixture(ctx, project)

    assert {:ok, blocked} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)
    assert Tickets.blocked?(ctx, blocked)

    assert {:ok, _done} = Tickets.transition(ctx, blocker.id, blocker.revision, :done)
    refute Tickets.blocked?(ctx, blocked)

    assert {:ok, after_done} = Tickets.fetch(ctx, blocked.id)
    assert after_done.revision == blocked.revision + 1
    assert Enum.count(event_types(project.id), &(&1 == "dependency.blocking_changed")) == 1

    assert {:ok, _ready} = Tickets.transition(ctx, blocker.id, blocker.revision + 1, :ready)
    assert Tickets.blocked?(ctx, after_done)

    assert {:ok, after_ready} = Tickets.fetch(ctx, blocked.id)
    assert after_ready.revision == after_done.revision + 1
    assert Enum.count(event_types(project.id), &(&1 == "dependency.blocking_changed")) == 2
  end

  test "done transition rejects unresolved blockers", %{ctx: ctx, project: project} do
    blocker = ticket_fixture(ctx, project)
    blocked = ticket_fixture(ctx, project)
    assert {:ok, blocked} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)

    assert {:error, %Error{kind: :blocked_by_dependency}} =
             Tickets.transition(ctx, blocked.id, blocked.revision, :done)
  end

  test "dependency public boundary validates authorization, UUIDs, and revisions before mutation",
       %{
         project: project
       } do
    ticket = ticket_fixture(Context.global(:me), project)
    invalid_ctx = %Context{actor: :system, scope: :global}

    assert {:error, %Error{kind: :unauthorized}} =
             Tickets.add_dependency(invalid_ctx, "bad", "bad", 0)

    assert {:error, %Error{kind: :validation_failed, fields: %{blocked_ticket_id: [_]}}} =
             Tickets.add_dependency(Context.global(:me), "bad", ticket.id, ticket.revision)

    assert {:error, %Error{kind: :validation_failed, fields: %{expected_revision: [_]}}} =
             Tickets.add_dependency(Context.global(:me), ticket.id, ticket.id, 0)
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

  defp event_types(project_id) do
    Event
    |> where([event], event.project_id == ^project_id)
    |> order_by([event], desc: event.id)
    |> select([event], event.event_type)
    |> Repo.all()
  end
end
