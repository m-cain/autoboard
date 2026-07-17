defmodule Autoboard.ReadModel do
  @moduledoc """
  Authorized, deterministic read projections over canonical Autoboard state.

  This module owns every read-side join, filter, and relationship aggregate. It
  returns domain structs and plain aggregate maps; transport code must use
  `Autoboard.Presenter` rather than serializing these values directly.
  """

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Attachments.Attachment
  alias Autoboard.Auth.Context
  alias Autoboard.Comments.Comment
  alias Autoboard.Domain.Error
  alias Autoboard.Projects.Project
  alias Autoboard.Repo
  alias Autoboard.Tickets
  alias Autoboard.Tickets.Dependency
  alias Autoboard.Tickets.Ticket

  @board_statuses [:backlog, :ready, :in_progress, :done]
  @default_activity_limit 100
  @max_search_limit 100

  @spec list_projects(Context.t()) ::
          {:ok, %{active: [Project.t()], archived: [Project.t()]}} | {:error, Error.t()}
  def list_projects(%Context{} = ctx) do
    with :ok <- authorize(ctx) do
      projects =
        Repo.all(
          from(project in Project,
            order_by: [asc: fragment("lower(?)", project.name), asc: project.id]
          )
        )

      {:ok,
       %{
         active: Enum.filter(projects, &(&1.state == :active)),
         archived: Enum.filter(projects, &(&1.state == :archived))
       }}
    end
  end

  def list_projects(_ctx), do: unauthorized()

  @spec triage_tickets(Context.t()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def triage_tickets(%Context{} = ctx) do
    with :ok <- authorize(ctx) do
      tickets =
        Ticket
        |> join(:inner, [ticket], project in Project, on: project.id == ticket.project_id)
        |> where([ticket, project], ticket.status == :triage and project.state == :active)
        |> ordered_tickets()
        |> Repo.all()
        |> hydrate_tickets()

      {:ok, tickets}
    end
  end

  def triage_tickets(_ctx), do: unauthorized()

  @spec project_board(Context.t(), String.t()) :: {:ok, map()} | {:error, Error.t()}
  def project_board(%Context{} = ctx, project_ref) do
    with :ok <- authorize(ctx),
         {:ok, project} <- fetch_project(project_ref) do
      tickets =
        Ticket
        |> where([ticket], ticket.project_id == ^project.id and ticket.status in ^@board_statuses)
        |> ordered_tickets()
        |> Repo.all()
        |> hydrate_tickets(project)

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
    with :ok <- authorize(ctx),
         {:ok, project} <- fetch_project(project_ref) do
      tickets =
        Ticket
        |> where([ticket], ticket.project_id == ^project.id and ticket.status == :canceled)
        |> ordered_tickets()
        |> Repo.all()
        |> hydrate_tickets(project)

      {:ok, tickets}
    end
  end

  def canceled_tickets(_ctx, _project_ref), do: unauthorized()

  @spec ticket_detail(Context.t(), String.t()) :: {:ok, map()} | {:error, Error.t()}
  def ticket_detail(%Context{} = ctx, ticket_ref) do
    with :ok <- authorize(ctx),
         {:ok, ticket} <- fetch_ticket(ticket_ref) do
      project = ticket.project
      parent = ticket.parent_ticket_id && Repo.get(Ticket, ticket.parent_ticket_id)

      subtasks =
        Repo.all(
          from(subtask in Ticket,
            where: subtask.parent_ticket_id == ^ticket.id,
            order_by: [asc: subtask.inserted_at, asc: subtask.id]
          )
        )

      blockers = related_tickets(ticket.id, :blockers)
      blocked_tickets = related_tickets(ticket.id, :blocked_tickets)

      tickets =
        hydrate_tickets(
          [ticket, parent | subtasks ++ blockers ++ blocked_tickets] |> Enum.reject(&is_nil/1),
          project
        )

      tickets_by_id = Map.new(tickets, &{&1.id, &1})

      {:ok,
       %{
         project: project,
         ticket: Map.fetch!(tickets_by_id, ticket.id),
         labels: Map.fetch!(tickets_by_id, ticket.id).labels,
         parent: ticket.parent_ticket_id && Map.get(tickets_by_id, ticket.parent_ticket_id),
         subtasks: ticket_refs(subtasks, tickets_by_id),
         blockers: ticket_refs(blockers, tickets_by_id),
         blocked_tickets: ticket_refs(blocked_tickets, tickets_by_id),
         comments: comments(ticket.id),
         attachments: attachments(ticket.id),
         activity: activity(ticket.id, @default_activity_limit),
         blocked: Enum.any?(blockers, &(&1.status not in [:done, :canceled]))
       }}
    end
  end

  def ticket_detail(_ctx, _ticket_ref), do: unauthorized()

  @spec search_tickets(Context.t(), map()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def search_tickets(%Context{} = ctx, attrs) do
    with :ok <- authorize(ctx),
         {:ok, attrs} <- canonical_attrs(attrs, ["query", "project_id", "limit"]),
         {:ok, query} <- required_query(attrs),
         {:ok, project_id} <- optional_uuid(Map.get(attrs, "project_id"), :project_id),
         {:ok, limit} <- search_limit(Map.get(attrs, "limit")) do
      pattern = "%#{query}%"

      tickets =
        Ticket
        |> maybe_filter_project(project_id)
        |> where([ticket], ilike(ticket.title, ^pattern) or ilike(ticket.description, ^pattern))
        |> ordered_tickets()
        |> limit(^limit)
        |> Repo.all()
        |> hydrate_tickets()

      {:ok, tickets}
    end
  end

  def search_tickets(_ctx, _attrs), do: unauthorized()

  @spec actionable_tickets(Context.t(), map()) :: {:ok, [Ticket.t()]} | {:error, Error.t()}
  def actionable_tickets(%Context{} = ctx, attrs) do
    with :ok <- authorize(ctx) do
      case Tickets.list_actionable(ctx, attrs) do
        tickets when is_list(tickets) -> {:ok, tickets}
        {:error, %Error{} = error} -> {:error, error}
      end
    end
  end

  def actionable_tickets(_ctx, _attrs), do: unauthorized()

  defp fetch_project(project_ref) do
    project =
      case Ecto.UUID.cast(project_ref) do
        {:ok, id} ->
          Repo.get(Project, id)

        :error when is_binary(project_ref) ->
          Repo.one(from(project in Project, where: project.key == ^String.upcase(project_ref)))

        :error ->
          nil
      end

    if project, do: {:ok, project}, else: not_found("project")
  end

  defp fetch_ticket(ticket_ref) do
    query =
      case Ecto.UUID.cast(ticket_ref) do
        {:ok, id} -> from(ticket in Ticket, where: ticket.id == ^id)
        :error -> ticket_identifier_query(ticket_ref)
      end

    ticket = query |> Repo.one() |> maybe_attach_project()
    if ticket, do: {:ok, ticket}, else: not_found("ticket")
  end

  defp ticket_identifier_query(ticket_ref) when is_binary(ticket_ref) do
    case Regex.run(~r/\A([A-Za-z][A-Za-z0-9]{1,7})-(\d+)\z/, ticket_ref) do
      [_, key, number] ->
        from(ticket in Ticket,
          join: project in Project,
          on: project.id == ticket.project_id,
          where:
            project.key == ^String.upcase(key) and ticket.number == ^String.to_integer(number)
        )

      _ ->
        from(ticket in Ticket, where: false)
    end
  end

  defp ticket_identifier_query(_ticket_ref), do: from(ticket in Ticket, where: false)

  defp maybe_attach_project(nil), do: nil

  defp maybe_attach_project(ticket) do
    project = Repo.get!(Project, ticket.project_id)
    %{ticket | project: project, identifier: ticket_identifier(ticket, project)}
  end

  defp related_tickets(ticket_id, :blockers) do
    Repo.all(
      from(blocker in Ticket,
        join: dependency in Dependency,
        on: dependency.blocker_ticket_id == blocker.id,
        where: dependency.blocked_ticket_id == ^ticket_id,
        order_by: [asc: blocker.inserted_at, asc: blocker.id]
      )
    )
  end

  defp related_tickets(ticket_id, :blocked_tickets) do
    Repo.all(
      from(blocked in Ticket,
        join: dependency in Dependency,
        on: dependency.blocked_ticket_id == blocked.id,
        where: dependency.blocker_ticket_id == ^ticket_id,
        order_by: [asc: blocked.inserted_at, asc: blocked.id]
      )
    )
  end

  defp comments(ticket_id),
    do:
      Repo.all(
        from(comment in Comment,
          where: comment.ticket_id == ^ticket_id,
          order_by: [asc: comment.inserted_at, asc: comment.id]
        )
      )

  defp attachments(ticket_id),
    do:
      Repo.all(
        from(attachment in Attachment,
          where: attachment.ticket_id == ^ticket_id,
          order_by: [asc: attachment.inserted_at, asc: attachment.id]
        )
      )

  defp activity(ticket_id, limit),
    do:
      Repo.all(
        from(event in Event,
          where: event.ticket_id == ^ticket_id,
          order_by: [desc: event.id],
          limit: ^limit
        )
      )

  defp hydrate_tickets(tickets, project \\ nil)
  defp hydrate_tickets([], _project), do: []

  defp hydrate_tickets(tickets, project) do
    tickets = Enum.uniq_by(tickets, & &1.id)
    project = project || Repo.get!(Project, hd(tickets).project_id)

    tickets
    |> Enum.map(&%{&1 | project: project, identifier: ticket_identifier(&1, project)})
    |> Repo.preload(:labels)
    |> Enum.map(fn ticket ->
      %{ticket | labels: Enum.sort_by(ticket.labels, &String.downcase(&1.name))}
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

  defp authorize(%Context{scope: :global, actor: actor}) when actor in [:me, :codex], do: :ok
  defp authorize(_ctx), do: unauthorized()

  defp unauthorized,
    do:
      {:error, %Error{kind: :unauthorized, message: "a global authorization context is required"}}

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
