defmodule Autoboard.CommentsTest do
  use Autoboard.DataCase, async: false

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Comments
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias Autoboard.Tickets

  setup do
    %{ctx: Context.global(:codex)}
  end

  test "adds an append-only comment using the authenticated actor and increments revision", %{
    ctx: ctx
  } do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)

    assert {:ok, comment} = Comments.add(ctx, ticket.id, %{body: "A useful note"})
    assert comment.actor == :codex
    assert comment.body == "A useful note"
    assert comment.ticket_id == ticket.id
    assert comment.project_id == project.id

    assert {:ok, updated_ticket} = Tickets.fetch(ctx, ticket.id)
    assert updated_ticket.revision == ticket.revision + 1

    assert %Event{event_type: "comment.added", actor: :codex, ticket_id: ticket_id} =
             Repo.one(
               from(event in Event,
                 where: event.ticket_id == ^ticket.id,
                 order_by: [desc: event.id],
                 limit: 1
               )
             )

    assert ticket_id == ticket.id
  end

  test "rejects blank comments without changing the ticket or activity log", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)

    assert {:error, %Error{kind: :validation_failed, fields: %{body: [_]}}} =
             Comments.add(ctx, ticket.id, %{body: " \n\t "})

    assert {:ok, unchanged} = Tickets.fetch(ctx, ticket.id)
    assert unchanged.revision == ticket.revision
    assert Repo.aggregate(Event, :count) == 2
  end

  test "preserves public authorization and archived project guards", %{ctx: ctx} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)

    assert {:error, %Error{kind: :unauthorized}} =
             Comments.add(%Context{actor: :system, scope: :global}, ticket.id, %{body: "Nope"})

    assert {:ok, archived} = Projects.archive(ctx, project.id, project.revision)

    assert {:error, %Error{kind: :validation_failed}} =
             Comments.add(ctx, ticket.id, %{body: "Nope"})

    assert archived.state == :archived
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} = Projects.create(ctx, %{key: key, name: "Project #{key}"})
    project
  end

  defp ticket_fixture(ctx, project) do
    assert {:ok, ticket} = Tickets.create(ctx, %{project_id: project.id, title: "Ticket"})
    ticket
  end
end
