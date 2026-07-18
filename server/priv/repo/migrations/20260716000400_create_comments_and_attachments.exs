defmodule Autoboard.Repo.Migrations.CreateCommentsAndAttachments do
  use Ecto.Migration

  def change do
    create table(:comments, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :project_id, references(:projects, type: :binary_id, on_delete: :restrict), null: false
      add :ticket_id, references(:tickets, type: :binary_id, on_delete: :restrict), null: false
      add :body, :text, null: false
      add :actor, :string, null: false

      timestamps(updated_at: false, type: :utc_datetime_usec)
    end

    create index(:comments, [:ticket_id, :inserted_at])

    create constraint(:comments, :comments_actor_check,
             check: "actor IN ('me', 'codex', 'system')"
           )

    create constraint(:comments, :comments_body_not_blank_check, check: "length(btrim(body)) > 0")

    create table(:attachments, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :project_id, references(:projects, type: :binary_id, on_delete: :restrict), null: false
      add :ticket_id, references(:tickets, type: :binary_id, on_delete: :restrict), null: false
      add :original_filename, :text, null: false
      add :media_type, :text, null: false
      add :byte_size, :bigint, null: false
      add :sha256, :string, null: false
      add :managed_path, :text, null: false
      add :actor, :string, null: false

      timestamps(updated_at: false, type: :utc_datetime_usec)
    end

    create index(:attachments, [:ticket_id, :inserted_at])
    create unique_index(:attachments, [:managed_path])

    create constraint(:attachments, :attachments_actor_check,
             check: "actor IN ('me', 'codex', 'system')"
           )

    create constraint(:attachments, :attachments_byte_size_check, check: "byte_size >= 0")
    create constraint(:attachments, :attachments_sha256_check, check: "sha256 ~ '^[0-9a-f]{64}$'")
  end
end
