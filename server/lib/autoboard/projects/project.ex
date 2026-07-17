defmodule Autoboard.Projects.Project do
  use Ecto.Schema

  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}

  schema "projects" do
    field(:key, :string)
    field(:name, :string)
    field(:description, :string, default: "")
    field(:state, Ecto.Enum, values: [active: "active", archived: "archived"], default: :active)
    field(:revision, :integer, default: 1)
    field(:next_ticket_number, :integer, default: 1)

    timestamps(type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}

  def create_changeset(project, attrs) do
    attrs = normalize_key(attrs)

    project
    |> cast(attrs, [:key, :name, :description])
    |> validate_required([:key, :name])
    |> validate_key()
    |> validate_name()
    |> validate_change(:description, fn :description, description ->
      if is_binary(description), do: [], else: [description: "must be a string"]
    end)
    |> unique_constraint(:key)
  end

  def update_changeset(project, attrs) do
    project
    |> cast(attrs, [:name, :description])
    |> reject_unsupported_fields(attrs)
    |> validate_name()
    |> validate_change(:description, fn :description, description ->
      if is_binary(description), do: [], else: [description: "must be a string"]
    end)
    |> require_user_field_change()
    |> increment_revision(project)
  end

  def state_changeset(project, state) when state in [:active, :archived] do
    project
    |> change(state: state, revision: project.revision + 1)
  end

  defp normalize_key(attrs) do
    attrs
    |> normalize_key(:key)
    |> normalize_key("key")
  end

  defp normalize_key(attrs, key) do
    case Map.fetch(attrs, key) do
      {:ok, value} when is_binary(value) -> Map.put(attrs, key, String.upcase(value))
      _ -> attrs
    end
  end

  defp validate_key(changeset) do
    validate_format(changeset, :key, ~r/^[A-Z][A-Z0-9]{1,7}$/,
      message: "must be 1-8 uppercase alphanumeric characters and begin with a letter"
    )
  end

  defp validate_name(changeset) do
    changeset
    |> validate_change(:name, fn :name, name ->
      trimmed = if is_binary(name), do: String.trim(name), else: ""

      cond do
        not is_binary(name) -> [name: "must be a string"]
        trimmed == "" -> [name: "must not be blank"]
        String.length(trimmed) > 200 -> [name: "must be at most 200 characters"]
        true -> []
      end
    end)
    |> update_change(:name, &String.trim/1)
  end

  defp reject_unsupported_fields(changeset, attrs) do
    Enum.reduce(attrs, changeset, fn {field, _value}, changeset ->
      case update_field(field) do
        :allowed -> changeset
        :key -> add_error(changeset, :key, "is immutable")
        :base -> add_error(changeset, :base, "#{inspect(field)} is not allowed")
        field when is_atom(field) -> add_error(changeset, field, "is not allowed")
      end
    end)
  end

  defp update_field(field) when field in [:name, :description, "name", "description"],
    do: :allowed

  defp update_field(field) when field in [:key, "key"], do: :key

  defp update_field(field)
       when field in [
              :state,
              :revision,
              :next_ticket_number,
              "state",
              "revision",
              "next_ticket_number"
            ],
       do: normalize_field(field)

  defp update_field(_field), do: :base

  defp normalize_field(field) when is_atom(field), do: field
  defp normalize_field(field), do: String.to_existing_atom(field)

  defp require_user_field_change(%{valid?: false} = changeset), do: changeset

  defp require_user_field_change(changeset) do
    if Map.take(changeset.changes, [:name, :description]) == %{} do
      add_error(changeset, :base, "must change at least one field")
    else
      changeset
    end
  end

  defp increment_revision(%{valid?: false} = changeset, _project), do: changeset

  defp increment_revision(changeset, project) do
    put_change(changeset, :revision, project.revision + 1)
  end
end
