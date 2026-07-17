defmodule Autoboard.Tickets.Label do
  use Ecto.Schema

  import Ecto.Changeset

  alias Autoboard.Projects.Project

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "labels" do
    field(:name, :string)
    belongs_to(:project, Project)

    timestamps(type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}

  def changeset(label, attrs) do
    label
    |> cast(attrs, [:project_id, :name])
    |> validate_required([:project_id, :name])
    |> validate_change(:name, fn :name, name ->
      trimmed = if is_binary(name), do: String.trim(name), else: ""

      cond do
        not is_binary(name) -> [name: "must be a string"]
        trimmed == "" -> [name: "must not be blank"]
        String.length(trimmed) > 50 -> [name: "must be at most 50 characters"]
        true -> []
      end
    end)
    |> update_change(:name, fn name -> name |> String.trim() |> String.replace(~r/\s+/, " ") end)
    |> unique_constraint(:name, name: :labels_project_id_name_index)
  end
end
