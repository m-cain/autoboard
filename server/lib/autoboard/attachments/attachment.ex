defmodule Autoboard.Attachments.Attachment do
  use Ecto.Schema

  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "attachments" do
    field(:original_filename, :string)
    field(:media_type, :string)
    field(:byte_size, :integer)
    field(:sha256, :string)
    field(:managed_path, :string)
    field(:actor, Ecto.Enum, values: [:me, :codex, :system])
    field(:project_id, :binary_id)
    field(:ticket_id, :binary_id)

    timestamps(updated_at: false, type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}

  def changeset(attachment, attrs) do
    attachment
    |> cast(attrs, [
      :original_filename,
      :media_type,
      :byte_size,
      :sha256,
      :managed_path,
      :actor,
      :project_id,
      :ticket_id
    ])
    |> validate_required([
      :original_filename,
      :media_type,
      :byte_size,
      :sha256,
      :managed_path,
      :actor,
      :project_id,
      :ticket_id
    ])
    |> validate_number(:byte_size, greater_than_or_equal_to: 0)
    |> validate_format(:sha256, ~r/\A[0-9a-f]{64}\z/)
  end
end
