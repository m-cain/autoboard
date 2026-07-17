defmodule Autoboard.Presenter do
  @moduledoc "Pure, JSON-safe transport presenters shared by RPC and HTTP."

  alias Autoboard.Activity.Event
  alias Autoboard.Attachments.Attachment
  alias Autoboard.Comments.Comment
  alias Autoboard.Domain.Error
  alias Autoboard.Projects.Project
  alias Autoboard.Tickets.Label
  alias Autoboard.Tickets.Ticket

  @spec project(Project.t()) :: map()
  def project(%Project{} = project) do
    %{
      "id" => project.id,
      "key" => project.key,
      "name" => project.name,
      "description" => project.description,
      "state" => enum(project.state),
      "revision" => project.revision,
      "inserted_at" => datetime(project.inserted_at),
      "updated_at" => datetime(project.updated_at)
    }
  end

  @spec ticket_summary(Ticket.t()) :: map()
  def ticket_summary(%Ticket{} = ticket) do
    %{
      "id" => ticket.id,
      "project_id" => ticket.project_id,
      "identifier" => ticket.identifier || ticket_identifier(ticket),
      "title" => ticket.title,
      "description" => ticket.description,
      "status" => enum(ticket.status),
      "priority" => enum(ticket.priority),
      "assignee" => enum(ticket.assignee),
      "revision" => ticket.revision,
      "parent_ticket_id" => ticket.parent_ticket_id,
      "blocked" => ticket.blocked || false,
      "comment_count" => ticket.comment_count || 0,
      "attachment_count" => ticket.attachment_count || 0,
      "labels" =>
        ticket.labels
        |> loaded_or_empty()
        |> Enum.sort_by(&String.downcase(&1.name))
        |> Enum.map(&label/1),
      "inserted_at" => datetime(ticket.inserted_at),
      "updated_at" => datetime(ticket.updated_at)
    }
  end

  @spec ticket_detail(map()) :: map()
  def ticket_detail(detail) when is_map(detail) do
    detail.ticket
    |> ticket_summary()
    |> Map.merge(%{
      "project" => project(detail.project),
      "blocked" => detail.blocked,
      "parent" => maybe_ticket(detail.parent),
      "subtasks" => Enum.map(detail.subtasks, &ticket_summary/1),
      "blockers" => Enum.map(detail.blockers, &ticket_summary/1),
      "blocked_tickets" => Enum.map(detail.blocked_tickets, &ticket_summary/1),
      "comments" => Enum.map(detail.comments, &comment/1),
      "attachments" => Enum.map(detail.attachments, &attachment/1),
      "activity" => Enum.map(detail.activity, &activity/1)
    })
  end

  @spec board(Project.t(), %{String.t() => [Ticket.t()]}) :: map()
  def board(%Project{} = project, grouped_tickets) when is_map(grouped_tickets) do
    %{
      "project" => project(project),
      "columns" =>
        Map.new(["backlog", "ready", "in_progress", "done"], fn status ->
          {status, grouped_tickets |> Map.get(status, []) |> Enum.map(&ticket_summary/1)}
        end)
    }
  end

  @spec activity(Event.t()) :: map()
  def activity(%Event{} = event) do
    %{
      "id" => event.id,
      "event_type" => event.event_type,
      "actor" => enum(event.actor),
      "project_id" => event.project_id,
      "ticket_id" => event.ticket_id,
      "payload" => json_value(event.payload),
      "inserted_at" => datetime(event.inserted_at)
    }
  end

  @spec attachment(Attachment.t(), boolean()) :: map()
  def attachment(%Attachment{} = attachment, include_managed_path? \\ false) do
    map = %{
      "id" => attachment.id,
      "project_id" => attachment.project_id,
      "ticket_id" => attachment.ticket_id,
      "original_filename" => attachment.original_filename,
      "media_type" => attachment.media_type,
      "byte_size" => attachment.byte_size,
      "sha256" => attachment.sha256,
      "actor" => enum(attachment.actor),
      "inserted_at" => datetime(attachment.inserted_at)
    }

    if include_managed_path?, do: Map.put(map, "managed_path", attachment.managed_path), else: map
  end

  @spec error(Error.t()) :: map()
  def error(%Error{} = error) do
    %{
      "kind" => enum(error.kind),
      "message" => error.message,
      "fields" => json_value(error.fields || %{}),
      "current" => current(error.current)
    }
  end

  defp comment(%Comment{} = comment) do
    %{
      "id" => comment.id,
      "project_id" => comment.project_id,
      "ticket_id" => comment.ticket_id,
      "body" => comment.body,
      "actor" => enum(comment.actor),
      "inserted_at" => datetime(comment.inserted_at)
    }
  end

  defp label(%Label{} = label),
    do: %{"id" => label.id, "name" => label.name, "project_id" => label.project_id}

  defp maybe_ticket(nil), do: nil
  defp maybe_ticket(ticket), do: ticket_summary(ticket)
  defp current(nil), do: nil
  defp current(%Project{} = project), do: project(project)
  defp current(%Ticket{} = ticket), do: ticket_summary(ticket)
  defp current(%Attachment{} = attachment), do: attachment(attachment)
  defp current(%Ecto.Association.NotLoaded{}), do: nil
  defp current(value) when is_struct(value), do: nil
  defp current(value) when is_map(value), do: sanitize_current_map(value)
  defp current(value) when is_list(value), do: Enum.map(value, &current/1)
  defp current(value), do: json_value(value)
  defp loaded_or_empty(%Ecto.Association.NotLoaded{}), do: []
  defp loaded_or_empty(labels), do: labels

  defp ticket_identifier(%Ticket{project: %Project{} = project, number: number}),
    do: "#{project.key}-#{number}"

  defp ticket_identifier(%Ticket{id: id}), do: id
  defp enum(nil), do: nil
  defp enum(value) when is_atom(value), do: Atom.to_string(value)
  defp enum(value), do: value
  defp datetime(nil), do: nil

  defp datetime(%DateTime{} = datetime) do
    datetime
    |> DateTime.to_unix(:microsecond)
    |> DateTime.from_unix!(:microsecond)
    |> DateTime.to_iso8601()
  end

  defp sanitize_current_map(value) do
    Enum.reduce(value, %{}, fn {key, nested}, sanitized ->
      key = to_string(key)

      if key in ["managed_path", "__struct__", "__meta__"] do
        sanitized
      else
        Map.put(sanitized, key, current(nested))
      end
    end)
  end

  defp json_value(value) when is_map(value),
    do: Map.new(value, fn {key, nested} -> {to_string(key), json_value(nested)} end)

  defp json_value(value) when is_list(value), do: Enum.map(value, &json_value/1)
  defp json_value(value) when is_atom(value), do: Atom.to_string(value)
  defp json_value(value), do: value
end
