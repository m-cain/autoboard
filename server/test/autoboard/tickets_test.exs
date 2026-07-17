defmodule Autoboard.TicketsTest do
  use Autoboard.DataCase, async: false

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias Autoboard.Tickets
  alias Autoboard.Tickets.Ticket

  setup do
    %{ctx: Context.global(:me)}
  end

  test "allocates project-local identifiers and defaults", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")

    assert {:ok, first} = Tickets.create(ctx, %{project_id: project.id, title: "First"})
    assert {:ok, second} = Tickets.create(ctx, %{project_id: project.id, title: "Second"})

    assert first.identifier == "AUTO-1"
    assert second.identifier == "AUTO-2"

    assert {first.status, first.priority, first.assignee, first.revision} ==
             {:triage, :none, :unassigned, 1}
  end

  test "allocates unique project-local numbers for concurrent creates", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")

    results =
      1..8
      |> Task.async_stream(
        fn number ->
          Tickets.create(ctx, %{project_id: project.id, title: "Ticket #{number}"})
        end,
        max_concurrency: 8,
        timeout: 15_000
      )
      |> Enum.map(fn {:ok, result} -> result end)

    assert Enum.all?(results, &match?({:ok, _}, &1))

    assert results
           |> Enum.map(fn {:ok, ticket} -> ticket.number end)
           |> Enum.sort() == Enum.to_list(1..8)
  end

  test "normalizes labels and replaces the complete label set", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")

    assert {:ok, ticket} =
             Tickets.create(ctx, %{
               project_id: project.id,
               title: "Label me",
               labels: ["  Needs   Review ", "needs review", "Backend"]
             })

    assert Enum.map(ticket.labels, & &1.name) |> Enum.sort() == ["Backend", "Needs Review"]

    assert {:ok, updated} =
             Tickets.update(ctx, ticket.id, ticket.revision, %{labels: [" backend ", "Urgent"]})

    assert Enum.map(updated.labels, & &1.name) |> Enum.sort() == ["Backend", "Urgent"]
    assert updated.revision == 2
  end

  test "stale ticket writes return the current ticket", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)

    assert {:ok, updated} = Tickets.update(ctx, ticket.id, ticket.revision, %{title: "Renamed"})

    assert {:error, %Error{kind: :revision_conflict, current: ^updated}} =
             Tickets.transition(ctx, ticket.id, ticket.revision, :ready)
  end

  test "rejects a grandchild", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    parent = ticket_fixture(ctx, project)
    child = ticket_fixture(ctx, project, %{parent_ticket_id: parent.id})

    assert {:error, %Error{kind: :validation_failed}} =
             Tickets.create(ctx, %{
               project_id: project.id,
               parent_ticket_id: child.id,
               title: "Too deep"
             })
  end

  test "rejects a parent from another project", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    other_project = project_fixture(ctx, "OTHR")
    parent = ticket_fixture(ctx, other_project)

    assert {:error, %Error{kind: :validation_failed, fields: %{parent_ticket_id: [_]}}} =
             Tickets.create(ctx, %{
               project_id: project.id,
               parent_ticket_id: parent.id,
               title: "Wrong project"
             })
  end

  test "terminal parent transitions reject non-terminal subtasks", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    parent = ticket_fixture(ctx, project)
    _child = ticket_fixture(ctx, project, %{parent_ticket_id: parent.id})

    for terminal_status <- [:done, :canceled] do
      assert {:error, %Error{kind: :invalid_transition}} =
               Tickets.transition(ctx, parent.id, parent.revision, terminal_status)
    end

    assert {:ok, unchanged} = Tickets.fetch(ctx, parent.id)
    assert unchanged.revision == parent.revision
    assert event_count(project.id) == 3
  end

  test "terminal parent transition succeeds after direct subtask is terminal", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    parent = ticket_fixture(ctx, project)
    child = ticket_fixture(ctx, project, %{parent_ticket_id: parent.id})

    assert {:ok, _child} = Tickets.transition(ctx, child.id, child.revision, :done)
    assert {:ok, done} = Tickets.transition(ctx, parent.id, parent.revision, :done)
    assert done.status == :done
  end

  test "accepts only the fixed status priority and assignee values", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")

    for attrs <- [
          %{status: :invalid},
          %{priority: :invalid},
          %{assignee: :invalid},
          %{status: 42},
          %{priority: 42},
          %{assignee: 42}
        ] do
      assert {:error, %Error{kind: :validation_failed}} =
               Tickets.create(ctx, Map.merge(%{project_id: project.id, title: "Invalid"}, attrs))
    end

    assert Repo.aggregate(Ticket, :count) == 0
    assert event_count(project.id) == 1
  end

  test "ticket mutations reject archived projects without consuming revisions or activity", %{
    ctx: ctx
  } do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    assert {:ok, archived} = Projects.archive(ctx, project.id, project.revision)

    assert {:error, %Error{kind: :validation_failed}} =
             Tickets.create(ctx, %{project_id: project.id, title: "Blocked"})

    assert {:error, %Error{kind: :validation_failed}} =
             Tickets.update(ctx, ticket.id, ticket.revision, %{title: "Blocked"})

    assert {:error, %Error{kind: :validation_failed}} =
             Tickets.transition(ctx, ticket.id, ticket.revision, :ready)

    assert {:ok, unchanged_ticket} = Tickets.fetch(ctx, ticket.id)
    assert unchanged_ticket.revision == ticket.revision
    assert {:ok, unchanged_project} = Projects.fetch(ctx, project.id)
    assert unchanged_project.revision == archived.revision
    assert event_count(project.id) == 3
  end

  test "update accepts only mutable fields and no-op writes are atomic", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)

    for attrs <- [%{}, %{status: :ready}, %{project_id: project.id}, %{parent_ticket_id: nil}] do
      assert {:error, %Error{kind: :validation_failed}} =
               Tickets.update(ctx, ticket.id, ticket.revision, attrs)
    end

    assert {:ok, unchanged} = Tickets.fetch(ctx, ticket.id)
    assert unchanged.revision == ticket.revision
    assert event_count(project.id) == 2
  end

  test "public ticket boundary validates authorization ids revisions attrs and text before mutation",
       %{
         ctx: ctx
       } do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    invalid_ctx = %Context{actor: :system, scope: :global}

    assert {:error, %Error{kind: :unauthorized}} = Tickets.create(invalid_ctx, [])

    assert {:error, %Error{kind: :validation_failed, fields: %{project_id: [_]}}} =
             Tickets.create(ctx, %{project_id: "invalid", title: "Nope"})

    assert {:error, %Error{kind: :validation_failed, fields: %{expected_revision: [_]}}} =
             Tickets.update(ctx, ticket.id, 0, %{title: "Nope"})

    assert {:error, %Error{kind: :validation_failed, fields: %{title: [_]}}} =
             Tickets.update(ctx, ticket.id, ticket.revision, %{title: nil})

    assert {:error, %Error{kind: :validation_failed, fields: %{base: [_ | _]}}} =
             Tickets.update(ctx, ticket.id, ticket.revision, %{"title" => "A", title: "B"})

    assert {:ok, unchanged} = Tickets.fetch(ctx, ticket.id)
    assert unchanged.revision == ticket.revision
  end

  test "fetch and search present virtual identifiers", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project, %{title: "Search phrase"})

    assert {:ok, fetched} = Tickets.fetch(ctx, ticket.id)
    assert fetched.identifier == "AUTO-1"

    assert {:ok, [found]} = Tickets.search(ctx, %{project_id: project.id, query: "phrase"})
    assert found.id == ticket.id
    assert found.identifier == "AUTO-1"
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} =
             Projects.create(ctx, %{key: key, name: "Project #{key}", description: ""})

    project
  end

  defp ticket_fixture(ctx, project, overrides \\ %{}) do
    assert {:ok, ticket} =
             Tickets.create(
               ctx,
               Map.merge(
                 %{project_id: project.id, title: "Ticket #{project.next_ticket_number}"},
                 overrides
               )
             )

    ticket
  end

  defp event_count(project_id) do
    Repo.aggregate(from(event in Event, where: event.project_id == ^project_id), :count)
  end
end
