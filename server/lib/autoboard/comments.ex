defmodule Autoboard.Comments do
  import Ecto.Query

  alias Autoboard.Activity
  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Scope
  alias Autoboard.Comments.Comment
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Repo

  @type result(value) :: {:ok, value} | {:error, Error.t()}

  @spec add(Context.t(), Ecto.UUID.t(), map()) :: result(Comment.t())
  def add(%Context{} = ctx, ticket_id, attrs) do
    with :ok <- Scope.authorize(ctx),
         {:ok, ticket_id} <- cast_uuid(ticket_id),
         {:ok, attrs} <- canonical_attrs(attrs) do
      Activity.commit(fn ->
        project = ticket_id |> ticket_project_id(ctx) |> locked_project_if_present(ctx)
        ticket = locked_ticket(ctx, ticket_id)

        with {:ok, ticket} <- require_ticket(ticket),
             {:ok, project} <- require_project(project),
             :ok <- Projects.ensure_active(project),
             {:ok, comment} <-
               %Comment{}
               |> Comment.changeset(%{
                 body: attrs["body"],
                 actor: ctx.actor,
                 project_id: project.id,
                 ticket_id: ticket.id
               })
               |> Repo.insert(),
             {:ok, _updated_ticket} <- increment_revision(ticket),
             {:ok, event} <-
               Activity.append(ctx, "comment.added", project.id, ticket.id, %{
                 "comment_id" => comment.id,
                 "body" => comment.body
               }) do
          {comment, [event]}
        else
          {:error, %Ecto.Changeset{} = changeset} -> Repo.rollback(validation_error(changeset))
          {:error, %Error{} = error} -> Repo.rollback(error)
        end
      end)
      |> result()
    end
  end

  def add(_ctx, _ticket_id, _attrs), do: unauthorized()

  defp ticket_project_id(ticket_id, ctx) do
    {:ok, tickets} = Scope.tickets(ctx)
    Repo.one(from(ticket in tickets, where: ticket.id == ^ticket_id, select: ticket.project_id))
  end

  defp locked_project_if_present(nil, _ctx), do: nil

  defp locked_project_if_present(id, ctx) do
    {:ok, projects} = Scope.projects(ctx)
    Repo.one(from(project in projects, where: project.id == ^id, lock: "FOR UPDATE"))
  end

  defp locked_ticket(ctx, id) do
    {:ok, tickets} = Scope.tickets(ctx)
    Repo.one(from(ticket in tickets, where: ticket.id == ^id, lock: "FOR UPDATE"))
  end

  defp require_project(nil), do: {:error, %Error{kind: :not_found, message: "project not found"}}
  defp require_project(project), do: {:ok, project}
  defp require_ticket(nil), do: {:error, %Error{kind: :not_found, message: "ticket not found"}}
  defp require_ticket(ticket), do: {:ok, ticket}

  defp increment_revision(ticket) do
    ticket |> Ecto.Changeset.change(revision: ticket.revision + 1) |> Repo.update()
  end

  defp canonical_attrs(attrs) when is_map(attrs) do
    case Enum.reduce(attrs, {%{}, []}, fn {key, value}, {canonical, errors} ->
           case key do
             key when is_atom(key) -> put_attr(canonical, errors, Atom.to_string(key), value)
             key when is_binary(key) -> put_attr(canonical, errors, key, value)
             _ -> {canonical, [{:base, "attribute key must be an atom or string"} | errors]}
           end
         end) do
      {canonical, []} ->
        if Map.keys(canonical) -- ["body"] == [],
          do: {:ok, canonical},
          else: invalid_argument(:base, "unsupported attribute")

      {_canonical, errors} ->
        {:error,
         %Error{
           kind: :validation_failed,
           message: "comment validation failed",
           fields: errors_by_pairs(errors)
         }}
    end
  end

  defp canonical_attrs(_), do: invalid_argument(:attrs, "must be a map")

  defp put_attr(attrs, errors, key, value) do
    if Map.has_key?(attrs, key),
      do: {attrs, [{:base, "duplicate attribute #{inspect(key)}"} | errors]},
      else: {Map.put(attrs, key, value), errors}
  end

  defp cast_uuid(id) do
    case Ecto.UUID.cast(id) do
      {:ok, id} -> {:ok, id}
      :error -> invalid_argument(:id, "must be a valid UUID")
    end
  end

  defp unauthorized, do: Scope.unauthorized("a global authorization context is required")

  defp invalid_argument(field, message),
    do:
      {:error,
       %Error{
         kind: :validation_failed,
         message: "comment validation failed",
         fields: %{field => [message]}
       }}

  defp result({:ok, value}), do: {:ok, value}
  defp result({:error, %Error{} = error}), do: {:error, error}
  defp result({:error, changeset}), do: {:error, validation_error(changeset)}

  defp validation_error(changeset),
    do: %Error{
      kind: :validation_failed,
      message: "comment validation failed",
      fields: errors_by_field(changeset)
    }

  defp errors_by_field(changeset),
    do: Ecto.Changeset.traverse_errors(changeset, fn {message, _options} -> message end)

  defp errors_by_pairs(pairs),
    do: pairs |> Enum.reverse() |> Enum.group_by(&elem(&1, 0), &elem(&1, 1))
end
