defmodule Autoboard.Repo.Migrations.CreateProjectsTokensAndActivity do
  use Ecto.Migration

  def up do
    execute("CREATE EXTENSION IF NOT EXISTS citext")

    create table(:projects, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :key, :citext, null: false
      add :name, :string, null: false
      add :description, :text, null: false, default: ""
      add :state, :string, null: false, default: "active"
      add :revision, :integer, null: false, default: 1
      add :next_ticket_number, :integer, null: false, default: 1

      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:projects, [:key])
    create constraint(:projects, :projects_state_check, check: "state IN ('active', 'archived')")

    create table(:access_tokens, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :digest, :binary, null: false
      add :actor, :string, null: false
      add :revoked_at, :utc_datetime_usec

      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:access_tokens, [:digest])

    create constraint(:access_tokens, :access_tokens_actor_check,
             check: "actor IN ('me', 'codex')"
           )

    create table(:activity_events, primary_key: false) do
      add :id, :bigserial, primary_key: true
      add :event_type, :string, null: false
      add :actor, :string, null: false
      add :project_id, references(:projects, type: :binary_id, on_delete: :restrict), null: false
      add :ticket_id, :binary_id
      add :payload, :map, null: false, default: %{}

      timestamps(updated_at: false, type: :utc_datetime_usec)
    end

    create index(:activity_events, [:project_id, :id])
    create index(:activity_events, [:ticket_id, :id])

    create constraint(:activity_events, :activity_events_actor_check,
             check: "actor IN ('me', 'codex', 'system')"
           )
  end

  def down do
    drop table(:activity_events)
    drop table(:access_tokens)
    drop table(:projects)
    execute("DROP EXTENSION IF EXISTS citext")
  end
end
