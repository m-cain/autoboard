defmodule Autoboard.ReadModel do
  @moduledoc """
  Authorized, deterministic read projections over canonical Autoboard state.

  This module owns every read-side join, filter, and relationship aggregate. It
  returns domain structs and plain aggregate maps; transport code must use
  `Autoboard.Presenter` rather than serializing these values directly.
  """

  import Ecto.Query

  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Scope
  alias Autoboard.Domain.Error
  alias Autoboard.Projects.Project
  alias Autoboard.Repo
  alias Autoboard.Tickets.Dependency
  alias Autoboard.Tickets.Ticket

  @board_statuses [:backlog, :ready, :in_progress, :done]
  @default_activity_limit 100
  @max_search_limit 100

  @spec list_projects(Context.t()) ::
          {:ok, %{active: [Project.t()], archived: [Project.t()]}} | {:error, Error.t()}
  def list_projects(%Context{} = ctx) do
    with {:ok, project_query} <- Scope.projects(ctx) do
      projects =
        project_query
        |> order_by([project], asc: fragment("lower(?)", project.name), asc: project.id)
        |> Repo.all()

      {:ok,
       %{
         active: Enum.filter(projects, &(&1.state == :active)),
         archived: Enum.filter(projects, &(&1.state == :archived))
       }}
    end
  end

  def list_projects(_ctx), do: unauthorized()

  @spec project(Context.t(), String.t()) :: {:ok, Project.t()} | {:error, Error.t()}
  def project(%Context{} = ctx, project_ref) do
    with :ok <- Scope.authorize(ctx), do: fetch_project(ctx, project_ref)
  end

  def project(_ctx, _project_ref), do: unauthorized()

  @spec triage_tickets(Context.t()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def triage_tickets(%Context{} = ctx) do
    with {:ok, ticket_query} <- Scope.tickets(ctx) do
      tickets =
        ticket_query
        |> join(:inner, [ticket], project in Project, on: project.id == ticket.project_id)
        |> where([ticket, project], ticket.status == :triage and project.state == :active)
        |> ordered_tickets()
        |> Repo.all()
        |> hydrate_tickets(ctx)

      {:ok, tickets}
    end
  end

  def triage_tickets(_ctx), do: unauthorized()

  @spec project_board(Context.t(), String.t()) :: {:ok, map()} | {:error, Error.t()}
  def project_board(%Context{} = ctx, project_ref) do
    with {:ok, ticket_query} <- Scope.tickets(ctx),
         {:ok, project} <- fetch_project(ctx, project_ref) do
      tickets =
        ticket_query
        |> where([ticket], ticket.project_id == ^project.id and ticket.status in ^@board_statuses)
        |> ordered_tickets()
        |> Repo.all()
        |> hydrate_tickets(ctx)

      {:ok,
       %{
         project: project,
         columns:
           Map.new(@board_statuses, fn status ->
             {Atom.to_string(status), Enum.filter(tickets, &(&1.status == status))}
           end)
       }}
    end
  end

  def project_board(_ctx, _project_ref), do: unauthorized()

  @spec canceled_tickets(Context.t(), String.t()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def canceled_tickets(%Context{} = ctx, project_ref) do
    with {:ok, ticket_query} <- Scope.tickets(ctx),
         {:ok, project} <- fetch_project(ctx, project_ref) do
      tickets =
        ticket_query
        |> where([ticket], ticket.project_id == ^project.id and ticket.status == :canceled)
        |> ordered_tickets()
        |> Repo.all()
        |> hydrate_tickets(ctx)

      {:ok, tickets}
    end
  end

  def canceled_tickets(_ctx, _project_ref), do: unauthorized()

  @spec ticket_detail(Context.t(), String.t()) :: {:ok, map()} | {:error, Error.t()}
  def ticket_detail(%Context{} = ctx, ticket_ref) do
    with {:ok, ticket_scope} <- Scope.tickets(ctx),
         {:ok, ticket} <- fetch_ticket(ctx, ticket_ref) do
      parent =
        ticket.parent_ticket_id &&
          Repo.one(from(parent in ticket_scope, where: parent.id == ^ticket.parent_ticket_id))

      subtasks =
        Repo.all(
          from(subtask in ticket_scope,
            where: subtask.parent_ticket_id == ^ticket.id,
            order_by: [asc: subtask.inserted_at, asc: subtask.id]
          )
        )

      blockers = related_tickets(ctx, ticket.id, :blockers)
      blocked_tickets = related_tickets(ctx, ticket.id, :blocked_tickets)

      tickets =
        hydrate_tickets(
          [ticket, parent | subtasks ++ blockers ++ blocked_tickets]
          |> Enum.reject(&is_nil/1),
          ctx
        )

      tickets_by_id = Map.new(tickets, &{&1.id, &1})
      ticket = Map.fetch!(tickets_by_id, ticket.id)

      {:ok,
       %{
         project: ticket.project,
         ticket: ticket,
         labels: ticket.labels,
         parent: ticket.parent_ticket_id && Map.get(tickets_by_id, ticket.parent_ticket_id),
         subtasks: ticket_refs(subtasks, tickets_by_id),
         blockers: ticket_refs(blockers, tickets_by_id),
         blocked_tickets: ticket_refs(blocked_tickets, tickets_by_id),
         comments: comments(ctx, ticket.id),
         attachments: attachments(ctx, ticket.id),
         activity: activity(ctx, ticket.id, @default_activity_limit),
         blocked: ticket.blocked
       }}
    end
  end

  def ticket_detail(_ctx, _ticket_ref), do: unauthorized()

  @spec search_tickets(Context.t(), map()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def search_tickets(%Context{} = ctx, attrs) do
    with {:ok, ticket_query} <- Scope.tickets(ctx),
         {:ok, attrs} <- canonical_attrs(attrs, ["query", "project_id", "limit"]),
         {:ok, query} <- required_query(attrs),
         {:ok, project_id} <- optional_uuid(Map.get(attrs, "project_id"), :project_id),
         {:ok, limit} <- search_limit(Map.get(attrs, "limit")) do
      pattern = "%#{escape_like(query)}%"

      tickets =
        ticket_query
        |> maybe_filter_project(project_id)
        |> where(
          [ticket],
          fragment("? ILIKE ? ESCAPE E'\\\\'", ticket.title, ^pattern) or
            fragment("? ILIKE ? ESCAPE E'\\\\'", ticket.description, ^pattern)
        )
        |> ordered_tickets()
        |> limit(^limit)
        |> Repo.all()
        |> hydrate_tickets(ctx)

      {:ok, tickets}
    end
  end

  def search_tickets(_ctx, _attrs), do: unauthorized()

  @spec actionable_tickets(Context.t(), map()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def actionable_tickets(%Context{} = ctx, attrs) do
    with {:ok, ticket_query} <- Scope.tickets(ctx, from(ticket in Ticket, as: :ticket)),
         {:ok, attrs} <- canonical_attrs(attrs, ["project_id", "limit"]),
         {:ok, project_id} <- optional_uuid(Map.get(attrs, "project_id"), :project_id),
         {:ok, limit} <- actionable_limit(Map.get(attrs, "limit")) do
      tickets =
        ticket_query
        |> join(:inner, [ticket], project in Project, on: project.id == ticket.project_id)
        |> where(
          [ticket, project],
          ticket.status == :ready and ticket.assignee == :codex and project.state == :active
        )
        |> maybe_filter_project(project_id)
        |> without_unresolved_blockers(ctx)
        |> without_non_terminal_subtasks(ctx)
        |> order_by([ticket],
          asc:
            fragment(
              "CASE ? WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END",
              ticket.priority
            ),
          asc: ticket.inserted_at,
          asc: ticket.id
        )
        |> limit(^limit)
        |> Repo.all()
        |> hydrate_tickets(ctx)

      {:ok, tickets}
    end
  end

  def actionable_tickets(_ctx, _attrs), do: unauthorized()

  defp fetch_project(ctx, project_ref) do
    {:ok, projects} = Scope.projects(ctx)

    project =
      case Ecto.UUID.cast(project_ref) do
        {:ok, id} ->
          Repo.one(from(project in projects, where: project.id == ^id))

        :error when is_binary(project_ref) ->
          Repo.one(from(project in projects, where: project.key == ^String.upcase(project_ref)))

        :error ->
          nil
      end

    if project, do: {:ok, project}, else: not_found("project")
  end

  defp fetch_ticket(ctx, ticket_ref) do
    {:ok, tickets} = Scope.tickets(ctx)

    query =
      case Ecto.UUID.cast(ticket_ref) do
        {:ok, id} -> from(ticket in tickets, where: ticket.id == ^id)
        :error -> ticket_identifier_query(tickets, ticket_ref)
      end

    ticket = Repo.one(query)
    if ticket, do: {:ok, ticket}, else: not_found("ticket")
  end

  defp ticket_identifier_query(tickets, ticket_ref) when is_binary(ticket_ref) do
    case Regex.run(~r/\A([A-Za-z][A-Za-z0-9]{1,7})-(\d+)\z/, ticket_ref) do
      [_, key, number] when byte_size(number) <= 10 ->
        case Integer.parse(number) do
          {number, ""} when number <= 2_147_483_647 ->
            from(ticket in tickets,
              join: project in Project,
              on: project.id == ticket.project_id,
              where: project.key == ^String.upcase(key) and ticket.number == ^number
            )

          _ ->
            from(ticket in tickets, where: false)
        end

      [_, _key, _number] ->
        from(ticket in tickets, where: false)

      _ ->
        from(ticket in tickets, where: false)
    end
  end

  defp ticket_identifier_query(tickets, _ticket_ref), do: from(ticket in tickets, where: false)

  defp related_tickets(ctx, ticket_id, :blockers) do
    {:ok, tickets} = Scope.tickets(ctx)

    Repo.all(
      from(blocker in tickets,
        join: dependency in Dependency,
        on: dependency.blocker_ticket_id == blocker.id,
        where: dependency.blocked_ticket_id == ^ticket_id,
        order_by: [asc: blocker.inserted_at, asc: blocker.id]
      )
    )
  end

  defp related_tickets(ctx, ticket_id, :blocked_tickets) do
    {:ok, tickets} = Scope.tickets(ctx)

    Repo.all(
      from(blocked in tickets,
        join: dependency in Dependency,
        on: dependency.blocked_ticket_id == blocked.id,
        where: dependency.blocker_ticket_id == ^ticket_id,
        order_by: [asc: blocked.inserted_at, asc: blocked.id]
      )
    )
  end

  defp comments(ctx, ticket_id) do
    {:ok, comments} = Scope.comments(ctx)

    Repo.all(
      from(comment in comments,
        where: comment.ticket_id == ^ticket_id,
        order_by: [asc: comment.inserted_at, asc: comment.id]
      )
    )
  end

  defp attachments(ctx, ticket_id) do
    {:ok, attachments} = Scope.attachments(ctx)

    Repo.all(
      from(attachment in attachments,
        where: attachment.ticket_id == ^ticket_id,
        order_by: [asc: attachment.inserted_at, asc: attachment.id]
      )
    )
  end

  defp activity(ctx, ticket_id, limit) do
    {:ok, events} = Scope.events(ctx)

    Repo.all(
      from(event in events,
        where: event.ticket_id == ^ticket_id,
        order_by: [desc: event.id],
        limit: ^limit
      )
    )
  end

  defp hydrate_tickets([], _ctx), do: []

  defp hydrate_tickets(tickets, ctx) do
    tickets = Enum.uniq_by(tickets, & &1.id)
    ticket_ids = Enum.map(tickets, & &1.id)

    {:ok, project_scope} = Scope.projects(ctx)

    projects_by_id =
      tickets
      |> Enum.map(& &1.project_id)
      |> Enum.uniq()
      |> then(fn project_ids ->
        Repo.all(from(project in project_scope, where: project.id in ^project_ids))
      end)
      |> Map.new(&{&1.id, &1})

    blocked_ids = unresolved_blocked_ticket_ids(ctx, ticket_ids)
    comment_counts = association_counts(ctx, :comments, ticket_ids)
    attachment_counts = association_counts(ctx, :attachments, ticket_ids)

    {:ok, label_scope} = Scope.labels(ctx)

    tickets
    |> Repo.preload(labels: label_scope)
    |> Enum.map(fn ticket ->
      project = Map.fetch!(projects_by_id, ticket.project_id)

      %{
        ticket
        | project: project,
          identifier: ticket_identifier(ticket, project),
          labels: Enum.sort_by(ticket.labels, &String.downcase(&1.name)),
          blocked: MapSet.member?(blocked_ids, ticket.id),
          comment_count: Map.get(comment_counts, ticket.id, 0),
          attachment_count: Map.get(attachment_counts, ticket.id, 0)
      }
    end)
  end

  defp ticket_refs(tickets, tickets_by_id),
    do: Enum.map(tickets, &Map.fetch!(tickets_by_id, &1.id))

  defp ticket_identifier(ticket, project), do: "#{project.key}-#{ticket.number}"

  defp ordered_tickets(query),
    do: order_by(query, [ticket], asc: ticket.inserted_at, asc: ticket.id)

  defp maybe_filter_project(query, nil), do: query

  defp maybe_filter_project(query, project_id),
    do: where(query, [ticket], ticket.project_id == ^project_id)

  defp unresolved_blocked_ticket_ids(ctx, ticket_ids) do
    {:ok, blockers} = Scope.tickets(ctx)

    ticket_ids
    |> then(fn ticket_ids ->
      Repo.all(
        from(blocker in blockers,
          join: dependency in Dependency,
          on: dependency.blocker_ticket_id == blocker.id,
          where:
            dependency.blocked_ticket_id in ^ticket_ids and
              blocker.status not in [:done, :canceled],
          select: dependency.blocked_ticket_id,
          distinct: true
        )
      )
    end)
    |> MapSet.new()
  end

  defp association_counts(ctx, association, ticket_ids) do
    {:ok, query} =
      case association do
        :comments -> Scope.comments(ctx)
        :attachments -> Scope.attachments(ctx)
      end

    Repo.all(
      from(record in query,
        where: record.ticket_id in ^ticket_ids,
        group_by: record.ticket_id,
        select: {record.ticket_id, count(record.id)}
      )
    )
    |> Map.new()
  end

  defp without_unresolved_blockers(query, ctx) do
    {:ok, blocker_scope} = Scope.tickets(ctx)

    blockers =
      from(blocker in blocker_scope,
        join: dependency in Dependency,
        on: dependency.blocker_ticket_id == blocker.id,
        where:
          dependency.blocked_ticket_id == parent_as(:ticket).id and
            blocker.status not in [:done, :canceled],
        select: 1
      )

    where(query, [_ticket], not exists(blockers))
  end

  defp without_non_terminal_subtasks(query, ctx) do
    {:ok, ticket_scope} = Scope.tickets(ctx)

    subtasks =
      from(subtask in ticket_scope,
        where:
          subtask.parent_ticket_id == parent_as(:ticket).id and
            subtask.status not in [:done, :canceled],
        select: 1
      )

    where(query, [_ticket], not exists(subtasks))
  end

  defp canonical_attrs(attrs, allowed) when is_map(attrs) do
    attrs = Map.new(attrs, fn {key, value} -> {to_string(key), value} end)
    unsupported = Map.keys(attrs) -- allowed

    if unsupported == [],
      do: {:ok, attrs},
      else: validation_error(:base, "#{inspect(hd(unsupported))} is not allowed")
  end

  defp canonical_attrs(_attrs, _allowed), do: validation_error(:attrs, "must be a map")
  defp required_query(%{"query" => query}) when is_binary(query), do: {:ok, query}
  defp required_query(_attrs), do: validation_error(:query, "must be a string")
  defp optional_uuid(nil, _field), do: {:ok, nil}

  defp optional_uuid(value, field) do
    case Ecto.UUID.cast(value) do
      {:ok, uuid} -> {:ok, uuid}
      :error -> validation_error(field, "must be a valid UUID")
    end
  end

  defp search_limit(nil), do: {:ok, @max_search_limit}

  defp search_limit(limit) when is_integer(limit) and limit in 1..@max_search_limit,
    do: {:ok, limit}

  defp search_limit(_limit),
    do: validation_error(:limit, "must be an integer from 1 to #{@max_search_limit}")

  defp actionable_limit(nil), do: {:ok, 25}

  defp actionable_limit(limit) when is_integer(limit) and limit in 1..100, do: {:ok, limit}

  defp actionable_limit(_limit),
    do: validation_error(:limit, "must be an integer from 1 to 100")

  defp escape_like(query) do
    query
    |> String.replace("\\", "\\\\")
    |> String.replace("%", "\\%")
    |> String.replace("_", "\\_")
  end

  defp unauthorized, do: Scope.unauthorized("a global authorization context is required")

  defp not_found(resource),
    do: {:error, %Error{kind: :not_found, message: "#{resource} not found"}}

  defp validation_error(field, message),
    do:
      {:error,
       %Error{
         kind: :validation_failed,
         message: "read model validation failed",
         fields: %{field => [message]}
       }}
end
