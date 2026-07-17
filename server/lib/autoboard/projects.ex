defmodule Autoboard.Projects do
  import Ecto.Query

  alias Autoboard.Activity
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects.Project
  alias Autoboard.Repo

  @type result(value) :: {:ok, value} | {:error, Error.t()}

  @spec create(Context.t(), map()) :: result(Project.t())
  def create(%Context{} = ctx, attrs) when is_map(attrs) do
    with :ok <- authorize(ctx),
         {:ok, {project, _events}} <-
           Repo.transaction(fn ->
             with {:ok, project} <- Repo.insert(Project.create_changeset(%Project{}, attrs)),
                  {:ok, event} <-
                    Activity.append(ctx, "project.created", project.id, nil, %{
                      "key" => project.key,
                      "name" => project.name
                    }) do
               {project, [event]}
             else
               {:error, %Ecto.Changeset{} = changeset} ->
                 Repo.rollback(validation_error(changeset))
             end
           end) do
      {:ok, project}
    end
  end

  def create(_ctx, _attrs), do: unauthorized()

  @spec update(Context.t(), Ecto.UUID.t(), pos_integer(), map()) :: result(Project.t())
  def update(%Context{} = ctx, id, expected_revision, attrs)
      when is_integer(expected_revision) and is_map(attrs) do
    with {:ok, id} <- cast_project_id(id) do
      mutate(
        ctx,
        id,
        expected_revision,
        "project.updated",
        attrs,
        &Project.update_changeset/2,
        fn project, changeset, _updated ->
          changed_field_payload(project, changeset, [:name, :description])
        end
      )
    end
  end

  def update(_ctx, _id, _expected_revision, _attrs), do: unauthorized()

  @spec archive(Context.t(), Ecto.UUID.t(), pos_integer()) :: result(Project.t())
  def archive(%Context{} = ctx, id, expected_revision) when is_integer(expected_revision) do
    with {:ok, id} <- cast_project_id(id) do
      mutate(
        ctx,
        id,
        expected_revision,
        "project.archived",
        :archive,
        fn project, _attrs -> Project.state_changeset(project, :archived) end,
        fn project, _changeset, _updated ->
          state_payload(project.state, :archived)
        end
      )
    end
  end

  def archive(_ctx, _id, _expected_revision), do: unauthorized()

  @spec restore(Context.t(), Ecto.UUID.t(), pos_integer()) :: result(Project.t())
  def restore(%Context{} = ctx, id, expected_revision) when is_integer(expected_revision) do
    with :ok <- authorize(ctx),
         {:ok, id} <- cast_project_id(id),
         {:ok, {project, _events}} <-
           Repo.transaction(fn ->
             project = locked_project(id)

             with {:ok, project} <- require_project(project),
                  :ok <- require_revision(project, expected_revision),
                  :ok <- require_archived(project),
                  {:ok, updated} <- Repo.update(Project.state_changeset(project, :active)),
                  {:ok, event} <-
                    Activity.append(ctx, "project.restored", updated.id, nil, %{
                      "state" => %{"from" => Atom.to_string(project.state), "to" => "active"}
                    }) do
               {updated, [event]}
             else
               {:error, %Ecto.Changeset{} = changeset} ->
                 Repo.rollback(validation_error(changeset))

               {:error, %Error{} = error} ->
                 Repo.rollback(error)
             end
           end) do
      {:ok, project}
    end
  end

  def restore(_ctx, _id, _expected_revision), do: unauthorized()

  @spec list(Context.t()) :: result([Project.t()])
  def list(%Context{} = ctx) do
    with :ok <- authorize(ctx) do
      {:ok,
       Repo.all(
         from(project in Project,
           order_by: [
             asc: fragment("CASE WHEN ? = 'active' THEN 0 ELSE 1 END", project.state),
             asc: fragment("lower(?)", project.name)
           ]
         )
       )}
    end
  end

  def list(_ctx), do: unauthorized()

  @spec fetch(Context.t(), Ecto.UUID.t()) :: result(Project.t())
  def fetch(%Context{} = ctx, id) do
    with :ok <- authorize(ctx),
         {:ok, id} <- cast_project_id(id) do
      Project
      |> Repo.get(id)
      |> require_project()
    end
  end

  def fetch(_ctx, _id), do: unauthorized()

  @spec ensure_active(Project.t()) :: :ok | {:error, Error.t()}
  def ensure_active(%Project{state: :active}), do: :ok

  def ensure_active(%Project{}) do
    {:error,
     %Error{
       kind: :validation_failed,
       message: "archived projects are read-only",
       fields: %{state: ["is archived"]}
     }}
  end

  defp mutate(
         ctx,
         id,
         expected_revision,
         event_type,
         attrs,
         changeset_fun,
         payload_fun,
         require_active? \\ true
       ) do
    with :ok <- authorize(ctx),
         {:ok, {project, _events}} <-
           Repo.transaction(fn ->
             project = locked_project(id)

             with {:ok, project} <- require_project(project),
                  :ok <- require_revision(project, expected_revision),
                  :ok <- guard_active(project, require_active?),
                  changeset = changeset_fun.(project, attrs),
                  {:ok, updated} <- Repo.update(changeset),
                  {:ok, event} <-
                    Activity.append(
                      ctx,
                      event_type,
                      updated.id,
                      nil,
                      payload_fun.(project, changeset, updated)
                    ) do
               {updated, [event]}
             else
               {:error, %Ecto.Changeset{} = changeset} ->
                 Repo.rollback(validation_error(changeset))

               {:error, %Error{} = error} ->
                 Repo.rollback(error)
             end
           end) do
      {:ok, project}
    end
  end

  defp locked_project(id) do
    Repo.one(from(project in Project, where: project.id == ^id, lock: "FOR UPDATE"))
  end

  defp cast_project_id(id) do
    case Ecto.UUID.cast(id) do
      {:ok, id} -> {:ok, id}
      :error -> invalid_project_id()
    end
  end

  defp invalid_project_id do
    {:error,
     %Error{
       kind: :validation_failed,
       message: "project validation failed",
       fields: %{id: ["must be a valid UUID"]}
     }}
  end

  defp require_project(nil), do: {:error, %Error{kind: :not_found, message: "project not found"}}
  defp require_project(project), do: {:ok, project}

  defp require_revision(%Project{revision: revision}, revision), do: :ok

  defp require_revision(project, _expected_revision) do
    {:error, %Error{kind: :revision_conflict, message: "project has changed", current: project}}
  end

  defp require_archived(%Project{state: :archived}), do: :ok

  defp require_archived(%Project{}) do
    {:error,
     %Error{
       kind: :validation_failed,
       message: "project is already active",
       fields: %{state: ["must be archived"]}
     }}
  end

  defp guard_active(project, true), do: ensure_active(project)
  defp guard_active(_project, false), do: :ok

  defp changed_field_payload(project, changeset, fields) do
    changeset.changes
    |> Map.take(fields)
    |> Map.new(fn {field, value} ->
      {Atom.to_string(field), %{"from" => Map.fetch!(project, field), "to" => value}}
    end)
  end

  defp state_payload(from, to) do
    %{"state" => %{"from" => Atom.to_string(from), "to" => Atom.to_string(to)}}
  end

  defp authorize(%Context{scope: :global, actor: actor}) when actor in [:me, :codex], do: :ok
  defp authorize(_ctx), do: unauthorized()

  defp unauthorized do
    {:error, %Error{kind: :unauthorized, message: "a global authorization context is required"}}
  end

  defp validation_error(changeset) do
    %Error{
      kind: :validation_failed,
      message: "project validation failed",
      fields: errors_by_field(changeset)
    }
  end

  defp errors_by_field(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, options} ->
      Enum.reduce(options, message, fn {key, value}, acc ->
        String.replace(acc, "%{#{key}}", to_string(value))
      end)
    end)
  end
end
