defmodule Autoboard.Repo.Migrations.CreateTicketDependencies do
  use Ecto.Migration

  def change do
    create table(:ticket_dependencies, primary_key: false) do
      add :blocker_ticket_id, references(:tickets, type: :binary_id, on_delete: :restrict),
        null: false

      add :blocked_ticket_id, references(:tickets, type: :binary_id, on_delete: :restrict),
        null: false

      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:ticket_dependencies, [:blocker_ticket_id, :blocked_ticket_id])
    create index(:ticket_dependencies, [:blocked_ticket_id])

    create constraint(:ticket_dependencies, :ticket_dependencies_no_self_edge_check,
             check: "blocker_ticket_id <> blocked_ticket_id"
           )
  end
end
