defmodule Autoboard.Auth.Token do
  use Ecto.Schema

  import Ecto.Changeset

  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Repo

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "access_tokens" do
    field(:digest, :binary)
    field(:actor, Ecto.Enum, values: [:me, :codex])
    field(:revoked_at, :utc_datetime_usec)

    timestamps(type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}
  @type issue_result :: {:ok, String.t(), t()} | {:error, Error.t()}

  def issue(actor) when actor in [:me, :codex] do
    plaintext = "ab_" <> Base.url_encode64(:crypto.strong_rand_bytes(32), padding: false)
    digest = :crypto.hash(:sha256, plaintext)

    %__MODULE__{}
    |> changeset(%{actor: actor, digest: digest})
    |> Repo.insert(log: false)
    |> case do
      {:ok, token} -> {:ok, plaintext, token}
      {:error, changeset} -> {:error, validation_error(changeset)}
    end
  end

  def issue(_actor),
    do: {:error, %Error{kind: :validation_failed, message: "invalid token actor"}}

  def authenticate(plaintext) when is_binary(plaintext) do
    digest = :crypto.hash(:sha256, plaintext)

    case Repo.get_by(__MODULE__, digest: digest) do
      %__MODULE__{revoked_at: nil, digest: stored_digest, actor: actor} ->
        if Plug.Crypto.secure_compare(stored_digest, digest) do
          {:ok, Context.global(actor)}
        else
          unauthorized()
        end

      _ ->
        unauthorized()
    end
  end

  def authenticate(_plaintext), do: unauthorized()

  def changeset(token, attrs) do
    token
    |> cast(attrs, [:digest, :actor, :revoked_at])
    |> validate_required([:digest, :actor])
    |> validate_change(:digest, fn :digest, digest ->
      if byte_size(digest) == 32, do: [], else: [digest: "must be 32 bytes"]
    end)
    |> unique_constraint(:digest)
  end

  defp unauthorized do
    {:error, %Error{kind: :unauthorized, message: "invalid or revoked access token"}}
  end

  defp validation_error(changeset) do
    %Error{
      kind: :validation_failed,
      message: "token validation failed",
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
