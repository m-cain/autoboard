defmodule Autoboard.Comments.Comment do
  use Ecto.Schema

  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "comments" do
    field(:body, :string)
    field(:actor, Ecto.Enum, values: [:me, :codex, :system])
    field(:project_id, :binary_id)
    field(:ticket_id, :binary_id)

    timestamps(updated_at: false, type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}

  def changeset(comment, attrs) do
    comment
    |> cast(attrs, [:body, :actor, :project_id, :ticket_id])
    |> validate_required([:body, :actor, :project_id, :ticket_id])
    |> validate_change(:body, fn :body, body ->
      trimmed = if is_binary(body), do: String.trim(body), else: ""

      cond do
        not is_binary(body) -> [body: "must be a string"]
        trimmed == "" -> [body: "must not be blank"]
        true -> []
      end
    end)
    |> update_change(:body, &String.trim/1)
  end
end
