defmodule Autoboard.Tickets.Dependency do
  use Ecto.Schema

  import Ecto.Changeset

  alias Autoboard.Tickets.Ticket

  @primary_key false
  @foreign_key_type :binary_id

  schema "ticket_dependencies" do
    belongs_to(:blocker_ticket, Ticket, primary_key: true)
    belongs_to(:blocked_ticket, Ticket, primary_key: true)

    timestamps(type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}

  def changeset(dependency, attrs) do
    dependency
    |> cast(attrs, [:blocker_ticket_id, :blocked_ticket_id])
    |> validate_required([:blocker_ticket_id, :blocked_ticket_id])
    |> unique_constraint([:blocker_ticket_id, :blocked_ticket_id])
    |> check_constraint(:blocker_ticket_id, name: :ticket_dependencies_no_self_edge_check)
  end
end
