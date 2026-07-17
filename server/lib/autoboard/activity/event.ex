defmodule Autoboard.Activity.Event do
  use Ecto.Schema

  import Ecto.Changeset

  @primary_key {:id, :id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "activity_events" do
    field(:event_type, :string)
    field(:actor, Ecto.Enum, values: [:me, :codex, :system])
    field(:project_id, :binary_id)
    field(:ticket_id, :binary_id)
    field(:payload, :map, default: %{})

    timestamps(updated_at: false, type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}

  def changeset(event, attrs) do
    event
    |> cast(attrs, [:event_type, :actor, :project_id, :ticket_id, :payload])
    |> validate_required([:event_type, :actor, :project_id, :payload])
  end
end
