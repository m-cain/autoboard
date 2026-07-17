defmodule Autoboard.Activity do
  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Repo

  @spec append(Context.t(), String.t(), Ecto.UUID.t(), Ecto.UUID.t() | nil, map()) ::
          {:ok, Event.t()} | {:error, Ecto.Changeset.t()}
  def append(%Context{} = ctx, event_type, project_id, ticket_id, payload)
      when is_binary(event_type) and is_binary(project_id) and
             (is_binary(ticket_id) or is_nil(ticket_id)) and
             is_map(payload) do
    %Event{}
    |> Event.changeset(%{
      actor: ctx.actor,
      event_type: event_type,
      project_id: project_id,
      ticket_id: ticket_id,
      payload: payload
    })
    |> Repo.insert()
  end
end
