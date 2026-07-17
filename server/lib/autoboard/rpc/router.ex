defmodule Autoboard.RPC.Router do
  @moduledoc false

  alias Autoboard.Attachments
  alias Autoboard.Auth.Context
  alias Autoboard.Comments
  alias Autoboard.Domain.Error
  alias Autoboard.Presenter
  alias Autoboard.Projects
  alias Autoboard.ReadModel
  alias Autoboard.Tickets

  @spec dispatch(Context.t(), String.t(), map()) :: {:ok, map()} | {:error, Error.t()}
  def dispatch(%Context{} = ctx, method, params) when is_binary(method) and is_map(params) do
    params = Map.drop(params, ["actor", "scope", :actor, :scope])
    params = stringify_keys(params)

    with :ok <- allowed_parameters(method, params) do
      route(ctx, method, params)
    end
  end

  def dispatch(_ctx, _method, _params), do: validation(:params, "must be an object")

  defp route(ctx, "projects.list", _params) do
    with {:ok, projects} <- ReadModel.list_projects(ctx) do
      {:ok,
       %{
         "active" => Enum.map(projects.active, &Presenter.project/1),
         "archived" => Enum.map(projects.archived, &Presenter.project/1)
       }}
    end
  end

  defp route(ctx, "projects.get", params) do
    with {:ok, project_ref} <- required(params, "project_id"),
         {:ok, project} <- ReadModel.project(ctx, project_ref) do
      {:ok, Presenter.project(project)}
    end
  end

  defp route(ctx, "projects.create", params) do
    with :ok <- require_keys(params, ["key", "name"]),
         {:ok, project} <- Projects.create(ctx, only(params, ["key", "name", "description"])) do
      {:ok, Presenter.project(project)}
    end
  end

  defp route(ctx, "projects.update", params) do
    with {:ok, project_id} <- required(params, "project_id"),
         {:ok, revision} <- revision(params),
         {:ok, project} <-
           Projects.update(ctx, project_id, revision, only(params, ["name", "description"])) do
      {:ok, Presenter.project(project)}
    end
  end

  defp route(ctx, method, params) when method in ["projects.archive", "projects.restore"] do
    with {:ok, project_id} <- required(params, "project_id"),
         {:ok, revision} <- revision(params),
         {:ok, project} <- project_state_mutation(method, ctx, project_id, revision) do
      {:ok, Presenter.project(project)}
    end
  end

  defp route(ctx, "tickets.board", params) do
    with {:ok, project_ref} <- required(params, "project_id"),
         {:ok, board} <- ReadModel.project_board(ctx, project_ref) do
      {:ok, Presenter.board(board.project, board.columns)}
    end
  end

  defp route(ctx, "tickets.search", params) do
    with {:ok, tickets} <-
           ReadModel.search_tickets(ctx, only(params, ["query", "project_id", "limit"])) do
      {:ok, %{"tickets" => Enum.map(tickets, &Presenter.ticket_summary/1)}}
    end
  end

  defp route(ctx, "tickets.get", params) do
    with {:ok, ticket_ref} <- required(params, "ticket_id"),
         {:ok, detail} <- ReadModel.ticket_detail(ctx, ticket_ref) do
      {:ok, Presenter.ticket_detail(detail)}
    end
  end

  defp route(ctx, "tickets.actionable", params) do
    with {:ok, tickets} <-
           ReadModel.actionable_tickets(ctx, only(params, ["project_id", "limit"])) do
      {:ok, %{"tickets" => Enum.map(tickets, &Presenter.ticket_summary/1)}}
    end
  end

  defp route(ctx, "tickets.create", params) do
    with :ok <- require_keys(params, ["project_id", "title"]),
         {:ok, params} <- resolve_optional_ticket(ctx, params, "parent_ticket_id"),
         {:ok, ticket} <-
           Tickets.create(
             ctx,
             only(params, [
               "project_id",
               "title",
               "description",
               "status",
               "priority",
               "assignee",
               "parent_ticket_id",
               "labels"
             ])
           ) do
      {:ok, Presenter.ticket_summary(ticket)}
    end
  end

  defp route(ctx, "tickets.update", params) do
    with {:ok, ticket_ref} <- required(params, "ticket_id"),
         {:ok, ticket_id} <- ticket_id(ctx, ticket_ref),
         {:ok, revision} <- revision(params),
         {:ok, ticket} <-
           Tickets.update(
             ctx,
             ticket_id,
             revision,
             only(params, ["title", "description", "priority", "assignee", "labels"])
           ) do
      {:ok, Presenter.ticket_summary(ticket)}
    end
  end

  defp route(ctx, "tickets.transition", params) do
    with {:ok, ticket_ref} <- required(params, "ticket_id"),
         {:ok, ticket_id} <- ticket_id(ctx, ticket_ref),
         {:ok, revision} <- revision(params),
         {:ok, status} <- required(params, "status"),
         {:ok, ticket} <- Tickets.transition(ctx, ticket_id, revision, status) do
      {:ok, Presenter.ticket_summary(ticket)}
    end
  end

  defp route(ctx, "comments.add", params) do
    with {:ok, ticket_ref} <- required(params, "ticket_id"),
         {:ok, ticket_id} <- ticket_id(ctx, ticket_ref),
         {:ok, body} <- required(params, "body"),
         {:ok, comment} <- Comments.add(ctx, ticket_id, %{"body" => body}) do
      {:ok,
       %{
         "id" => comment.id,
         "ticket_id" => comment.ticket_id,
         "project_id" => comment.project_id,
         "body" => comment.body,
         "actor" => Atom.to_string(comment.actor),
         "inserted_at" => DateTime.to_iso8601(comment.inserted_at)
       }}
    end
  end

  defp route(ctx, "attachments.add_from_path", params) do
    with {:ok, ticket_ref} <- required(params, "ticket_id"),
         {:ok, ticket_id} <- ticket_id(ctx, ticket_ref),
         {:ok, path} <- required(params, "path"),
         true <- is_binary(path) || validation(:path, "must be a string"),
         {:ok, attachment} <- Attachments.add_from_path(ctx, ticket_id, path) do
      {:ok, Presenter.attachment(attachment)}
    else
      false -> validation(:path, "must be a string")
      {:error, %Error{} = error} -> {:error, error}
    end
  end

  defp route(ctx, "attachments.read", params) do
    with {:ok, attachment_id} <- required(params, "attachment_id"),
         {:ok, read} <- Attachments.read(ctx, attachment_id) do
      result = %{"attachment" => Presenter.attachment(read.attachment, true)}

      {:ok,
       if(Map.has_key?(read, :content),
         do: Map.put(result, "content", read.content),
         else: Map.put(result, "managed_path", read.managed_path)
       )}
    end
  end

  defp route(ctx, method, params) when method in ["dependencies.add", "dependencies.remove"] do
    with {:ok, blocked_ref} <- required(params, "blocked_ticket_id"),
         {:ok, blocked_id} <- ticket_id(ctx, blocked_ref),
         {:ok, blocker_ref} <- required(params, "blocker_ticket_id"),
         {:ok, blocker_id} <- ticket_id(ctx, blocker_ref),
         {:ok, revision} <- revision(params),
         {:ok, ticket} <- dependency_mutation(method, ctx, blocked_id, blocker_id, revision) do
      {:ok, Presenter.ticket_summary(ticket)}
    end
  end

  defp route(_ctx, _method, _params),
    do: {:error, %Error{kind: :method_not_found, message: "method not found"}}

  defp project_state_mutation("projects.archive", ctx, id, revision),
    do: Projects.archive(ctx, id, revision)

  defp project_state_mutation("projects.restore", ctx, id, revision),
    do: Projects.restore(ctx, id, revision)

  defp dependency_mutation("dependencies.add", ctx, blocked, blocker, revision),
    do: Tickets.add_dependency(ctx, blocked, blocker, revision)

  defp dependency_mutation("dependencies.remove", ctx, blocked, blocker, revision),
    do: Tickets.remove_dependency(ctx, blocked, blocker, revision)

  defp allowed_parameters("projects.list", params), do: allowed(params, [])
  defp allowed_parameters("projects.get", params), do: allowed(params, ["project_id"])

  defp allowed_parameters("projects.create", params),
    do: allowed(params, ["key", "name", "description"])

  defp allowed_parameters("projects.update", params),
    do: allowed(params, ["project_id", "expected_revision", "name", "description"])

  defp allowed_parameters(method, params) when method in ["projects.archive", "projects.restore"],
    do: allowed(params, ["project_id", "expected_revision"])

  defp allowed_parameters("tickets.board", params), do: allowed(params, ["project_id"])

  defp allowed_parameters("tickets.search", params),
    do: allowed(params, ["query", "project_id", "limit"])

  defp allowed_parameters("tickets.get", params), do: allowed(params, ["ticket_id"])

  defp allowed_parameters("tickets.actionable", params),
    do: allowed(params, ["project_id", "limit"])

  defp allowed_parameters("tickets.create", params),
    do:
      allowed(params, [
        "project_id",
        "title",
        "description",
        "status",
        "priority",
        "assignee",
        "parent_ticket_id",
        "labels"
      ])

  defp allowed_parameters("tickets.update", params),
    do:
      allowed(params, [
        "ticket_id",
        "expected_revision",
        "title",
        "description",
        "priority",
        "assignee",
        "labels"
      ])

  defp allowed_parameters("tickets.transition", params),
    do: allowed(params, ["ticket_id", "expected_revision", "status"])

  defp allowed_parameters("comments.add", params), do: allowed(params, ["ticket_id", "body"])

  defp allowed_parameters("attachments.add_from_path", params),
    do: allowed(params, ["ticket_id", "path"])

  defp allowed_parameters("attachments.read", params), do: allowed(params, ["attachment_id"])

  defp allowed_parameters(method, params)
       when method in ["dependencies.add", "dependencies.remove"],
       do: allowed(params, ["blocked_ticket_id", "blocker_ticket_id", "expected_revision"])

  defp allowed_parameters(_method, _params), do: :ok

  defp allowed(params, allowed_keys) do
    case Map.keys(params) -- allowed_keys do
      [] -> :ok
      [key | _] -> validation(:base, "#{inspect(key)} is not allowed")
    end
  end

  defp ticket_id(ctx, ticket_ref) do
    with {:ok, detail} <- ReadModel.ticket_detail(ctx, ticket_ref), do: {:ok, detail.ticket.id}
  end

  defp resolve_optional_ticket(ctx, params, key) do
    case Map.fetch(params, key) do
      :error -> {:ok, params}
      {:ok, nil} -> {:ok, params}
      {:ok, ref} -> with {:ok, id} <- ticket_id(ctx, ref), do: {:ok, Map.put(params, key, id)}
    end
  end

  defp revision(params) do
    case Map.get(params, "expected_revision") do
      revision when is_integer(revision) and revision > 0 -> {:ok, revision}
      _ -> validation(:expected_revision, "must be a positive integer")
    end
  end

  defp require_keys(params, keys) do
    case Enum.find(keys, &(not Map.has_key?(params, &1))) do
      nil -> :ok
      key -> validation(String.to_atom(key), "is required")
    end
  end

  defp required(params, key) do
    case Map.get(params, key) do
      value when is_binary(value) and value != "" -> {:ok, value}
      _ -> validation(String.to_atom(key), "is required and must be a string")
    end
  end

  defp only(params, keys), do: Map.take(params, keys)
  defp stringify_keys(params), do: Map.new(params, fn {key, value} -> {to_string(key), value} end)

  defp validation(field, message),
    do:
      {:error,
       %Error{
         kind: :validation_failed,
         message: "invalid RPC parameters",
         fields: %{field => [message]}
       }}
end
