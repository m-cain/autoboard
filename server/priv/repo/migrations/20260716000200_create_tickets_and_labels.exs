defmodule Autoboard.Repo.Migrations.CreateTicketsAndLabels do
  use Ecto.Migration

  def up do
    create table(:tickets, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :project_id, references(:projects, type: :binary_id, on_delete: :restrict), null: false
      add :number, :integer, null: false
      add :title, :string, null: false
      add :description, :text, null: false, default: ""
      add :status, :string, null: false, default: "triage"
      add :priority, :string, null: false, default: "none"
      add :assignee, :string, null: false, default: "unassigned"
      add :revision, :integer, null: false, default: 1
      add :parent_ticket_id, references(:tickets, type: :binary_id, on_delete: :restrict)

      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:tickets, [:project_id, :number])
    create index(:tickets, [:project_id, :status])
    create index(:tickets, [:project_id, :assignee])
    create index(:tickets, [:parent_ticket_id])
    create constraint(:tickets, :tickets_positive_number_check, check: "number > 0")
    create constraint(:tickets, :tickets_status_check,
             check: "status IN ('triage', 'backlog', 'ready', 'in_progress', 'done', 'canceled')"
           )
    create constraint(:tickets, :tickets_priority_check,
             check: "priority IN ('none', 'low', 'medium', 'high', 'urgent')"
           )
    create constraint(:tickets, :tickets_assignee_check,
             check: "assignee IN ('unassigned', 'me', 'codex')"
           )
    create constraint(:tickets, :tickets_no_self_parent_check,
             check: "parent_ticket_id IS NULL OR parent_ticket_id <> id"
           )

    create table(:labels, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :project_id, references(:projects, type: :binary_id, on_delete: :restrict), null: false
      add :name, :citext, null: false

      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:labels, [:project_id, :name])

    create table(:ticket_labels, primary_key: false) do
      add :ticket_id, references(:tickets, type: :binary_id, on_delete: :delete_all), null: false
      add :label_id, references(:labels, type: :binary_id, on_delete: :restrict), null: false
    end

    create unique_index(:ticket_labels, [:ticket_id, :label_id])

    execute("""
    ALTER TABLE activity_events
    ADD CONSTRAINT activity_events_ticket_id_fkey
    FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE RESTRICT
    """)
  end

  def down do
    execute("ALTER TABLE activity_events DROP CONSTRAINT IF EXISTS activity_events_ticket_id_fkey")
    drop table(:ticket_labels)
    drop table(:labels)
    drop table(:tickets)
  end
end
