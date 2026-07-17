defmodule Autoboard.Auth.ScopeIntegrationTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Activity
  alias Autoboard.Attachments
  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Scope
  alias Autoboard.Comments
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.ReadModel
  alias Autoboard.Repo
  alias Autoboard.Tickets
  alias Autoboard.Tickets.Dependency

  setup do
    global = Context.global(:codex)
    alpha = project_fixture(global, "ALPHA")
    beta = project_fixture(global, "BETA")

    alpha_ticket =
      ticket_fixture(global, alpha, %{title: "Scoped alpha", status: :ready, assignee: :codex})

    beta_ticket =
      ticket_fixture(global, beta, %{title: "Scoped beta", status: :ready, assignee: :codex})

    scoped = Context.project(:codex, alpha.id)

    %{
      global: global,
      scoped: scoped,
      alpha: alpha,
      beta: beta,
      alpha_ticket: alpha_ticket,
      beta_ticket: beta_ticket
    }
  end

  test "project contexts and central query entrypoints constrain canonical roots", %{
    scoped: scoped,
    alpha: alpha
  } do
    assert %Context{scope: {:project, project_id}} = scoped
    assert project_id == alpha.id
    assert :ok = Scope.authorize(scoped)
    assert {:ok, project_query} = Scope.projects(scoped)
    assert [project] = Repo.all(project_query)
    assert project.id == alpha.id
    assert {:ok, ticket_query} = Scope.tickets(scoped)
    assert Enum.all?(Repo.all(ticket_query), &(&1.project_id == alpha.id))
  end

  test "project, ticket, board, triage, search, and actionable reads never cross scope",
       context do
    %{
      scoped: scoped,
      alpha: alpha,
      beta: beta,
      alpha_ticket: alpha_ticket,
      beta_ticket: beta_ticket
    } = context

    assert {:ok, [listed]} = Projects.list(scoped)
    assert listed.id == alpha.id
    assert_not_found(Projects.fetch(scoped, beta.id))

    assert {:ok, %{active: [read_project], archived: []}} = ReadModel.list_projects(scoped)
    assert read_project.id == alpha.id
    assert_not_found(ReadModel.project(scoped, beta.key))
    assert_not_found(ReadModel.project_board(scoped, beta.key))
    assert_not_found(ReadModel.canceled_tickets(scoped, beta.key))
    assert_not_found(ReadModel.ticket_detail(scoped, beta_ticket.identifier))

    assert {:ok, board} = ReadModel.project_board(scoped, alpha.key)
    assert Enum.map(board.columns["ready"], & &1.id) == [alpha_ticket.id]
    assert {:ok, triage} = ReadModel.triage_tickets(scoped)
    assert Enum.all?(triage, &(&1.project_id == alpha.id))
    assert {:ok, [search]} = ReadModel.search_tickets(scoped, %{query: "Scoped"})
    assert search.id == alpha_ticket.id
    assert {:ok, []} = ReadModel.search_tickets(scoped, %{query: "Scoped", project_id: beta.id})
    assert {:ok, [actionable]} = ReadModel.actionable_tickets(scoped, %{limit: 100})
    assert actionable.id == alpha_ticket.id
    assert {:ok, []} = ReadModel.actionable_tickets(scoped, %{project_id: beta.id, limit: 100})

    assert_not_found(Tickets.fetch(scoped, beta_ticket.id))
    assert {:ok, [search]} = Tickets.search(scoped, %{query: "Scoped"})
    assert search.id == alpha_ticket.id
    assert Enum.map(Tickets.list_actionable(scoped, %{limit: 100}), & &1.id) == [alpha_ticket.id]
  end

  test "representative mutations cannot address another project", context do
    %{
      scoped: scoped,
      alpha: alpha,
      beta: beta,
      alpha_ticket: alpha_ticket,
      beta_ticket: beta_ticket
    } = context

    assert {:error, %Error{kind: :unauthorized}} =
             Projects.create(scoped, %{key: "NOPE", name: "Nope"})

    assert_not_found(Projects.update(scoped, beta.id, beta.revision, %{name: "Leaked"}))
    assert_not_found(Projects.archive(scoped, beta.id, beta.revision))
    assert_not_found(Projects.restore(scoped, beta.id, beta.revision))

    assert_not_found(Tickets.create(scoped, %{project_id: beta.id, title: "Leaked"}))

    assert_not_found(
      Tickets.update(scoped, beta_ticket.id, beta_ticket.revision, %{title: "Leaked"})
    )

    assert_not_found(
      Tickets.transition(scoped, beta_ticket.id, beta_ticket.revision, :in_progress)
    )

    assert_not_found(Comments.add(scoped, beta_ticket.id, %{body: "Leaked"}))

    source = Path.join(System.tmp_dir!(), "autoboard-scope-#{Ecto.UUID.generate()}.txt")
    File.write!(source, "scope boundary")
    on_exit(fn -> File.rm(source) end)
    assert_not_found(Attachments.add_from_path(scoped, beta_ticket.id, source))

    assert {:ok, beta_attachment} =
             Attachments.add_from_path(context.global, beta_ticket.id, source)

    assert_not_found(Attachments.fetch(scoped, beta_attachment.id))
    assert_not_found(Attachments.read(scoped, beta_attachment.id))

    assert_not_found(
      Tickets.add_dependency(scoped, alpha_ticket.id, beta_ticket.id, alpha_ticket.revision)
    )

    assert {:error, %Error{kind: :validation_failed, fields: %{parent_ticket_id: [_]}}} =
             Tickets.create(scoped, %{
               project_id: alpha.id,
               parent_ticket_id: beta_ticket.id,
               title: "Cross-scope child"
             })

    refute Repo.exists?(Dependency)
    assert {:ok, unchanged} = Tickets.fetch(scoped, alpha_ticket.id)
    assert unchanged.revision == alpha_ticket.revision

    assert {:ok, own_comment} = Comments.add(scoped, alpha_ticket.id, %{body: "Allowed"})
    assert own_comment.project_id == alpha.id
  end

  test "activity replay is filtered to the scoped project", %{
    global: global,
    scoped: scoped,
    alpha: alpha
  } do
    assert {:ok, global_events} = Activity.replay_after(global, 0)
    assert Enum.any?(global_events, &(&1.project_id != alpha.id))
    assert {:ok, scoped_events} = Activity.replay_after(scoped, 0)
    assert scoped_events != []
    assert Enum.all?(scoped_events, &(&1.project_id == alpha.id))
    assert {:ok, scoped_high_water} = Activity.high_water(scoped)
    assert scoped_high_water == Enum.max(Enum.map(scoped_events, & &1.id))
  end

  defp assert_not_found(result), do: assert({:error, %Error{kind: :not_found}} = result)

  defp project_fixture(ctx, key) do
    assert {:ok, project} =
             Projects.create(ctx, %{key: key, name: "#{key} project", description: ""})

    project
  end

  defp ticket_fixture(ctx, project, attrs) do
    assert {:ok, ticket} = Tickets.create(ctx, Map.merge(%{project_id: project.id}, attrs))
    ticket
  end
end
