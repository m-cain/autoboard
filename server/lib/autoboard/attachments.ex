defmodule Autoboard.Attachments do
  import Ecto.Query

  require Logger

  alias Autoboard.Activity
  alias Autoboard.Attachments.Attachment
  alias Autoboard.Attachments.Storage
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Projects.Project
  alias Autoboard.Repo
  alias Autoboard.Tickets.Ticket

  @inline_limit 262_144
  @type result(value) :: {:ok, value} | {:error, Error.t()}

  @spec add_from_path(Context.t(), Ecto.UUID.t(), String.t()) :: result(Attachment.t())
  def add_from_path(%Context{} = ctx, ticket_id, source_path) do
    with :ok <- authorize(ctx),
         {:ok, ticket_id} <- cast_uuid(ticket_id, :id),
         {:ok, staged} <- Storage.stage(source_path),
         attachment_id <- Ecto.UUID.generate(),
         final_path <- Storage.final_path(attachment_id),
         :ok <- move_staged_file(staged.staged_path, final_path) do
      result =
        Activity.commit(fn ->
          project = ticket_id |> ticket_project_id() |> locked_project_if_present()
          ticket = locked_ticket(ticket_id)

          with {:ok, ticket} <- require_ticket(ticket),
               {:ok, project} <- require_project(project),
               :ok <- Projects.ensure_active(project),
               {:ok, attachment} <-
                 %Attachment{id: attachment_id}
                 |> Attachment.changeset(%{
                   original_filename: staged.original_filename,
                   media_type: staged.media_type,
                   byte_size: staged.byte_size,
                   sha256: staged.sha256,
                   managed_path: final_path,
                   actor: ctx.actor,
                   project_id: project.id,
                   ticket_id: ticket.id
                 })
                 |> Repo.insert(),
               {:ok, _updated_ticket} <- increment_revision(ticket),
               {:ok, event} <-
                 Activity.append(ctx, "attachment.added", project.id, ticket.id, %{
                   "attachment_id" => attachment.id,
                   "original_filename" => attachment.original_filename,
                   "media_type" => attachment.media_type,
                   "byte_size" => attachment.byte_size,
                   "sha256" => attachment.sha256
                 }) do
            {attachment, [event]}
          else
            {:error, %Ecto.Changeset{} = changeset} -> Repo.rollback(validation_error(changeset))
            {:error, %Error{} = error} -> Repo.rollback(error)
          end
        end)

      case result do
        {:ok, attachment} ->
          {:ok, attachment}

        {:error, error} ->
          File.rm(final_path)
          {:error, error}
      end
    else
      {:error, reason} -> {:error, storage_error(reason)}
    end
  end

  def add_from_path(_ctx, _ticket_id, _source_path), do: unauthorized()

  @spec fetch(Context.t(), Ecto.UUID.t()) :: result(Attachment.t())
  def fetch(%Context{} = ctx, attachment_id) do
    with :ok <- authorize(ctx),
         {:ok, attachment_id} <- cast_uuid(attachment_id, :id) do
      case Repo.get(Attachment, attachment_id) do
        nil -> {:error, %Error{kind: :not_found, message: "attachment not found"}}
        attachment -> {:ok, attachment}
      end
    end
  end

  def fetch(_ctx, _attachment_id), do: unauthorized()

  @spec read(Context.t(), Ecto.UUID.t()) :: result(map())
  def read(%Context{} = ctx, attachment_id) do
    with {:ok, attachment} <- fetch(ctx, attachment_id),
         :ok <- require_managed_regular_file(attachment.managed_path) do
      if text_attachment?(attachment) and attachment.byte_size <= @inline_limit do
        case File.read(attachment.managed_path) do
          {:ok, content} ->
            if String.valid?(content) do
              {:ok, %{attachment: attachment, content: content}}
            else
              {:ok, %{attachment: attachment, managed_path: attachment.managed_path}}
            end

          {:error, _reason} ->
            {:error, storage_error(:unreadable)}
        end
      else
        {:ok, %{attachment: attachment, managed_path: attachment.managed_path}}
      end
    end
  end

  def read(_ctx, _attachment_id), do: unauthorized()

  @spec cleanup() :: :ok
  def cleanup do
    cleanup_stale_temp_files()

    try do
      log_orphan_final_files()
    rescue
      DBConnection.OwnershipError -> :ok
      Postgrex.Error -> :ok
    end

    :ok
  end

  defp move_staged_file(staged_path, final_path) do
    with :ok <- File.mkdir_p(Storage.final_dir()),
         :ok <- File.rename(staged_path, final_path) do
      :ok
    else
      {:error, _reason} ->
        File.rm(staged_path)
        {:error, :unreadable}
    end
  end

  defp cleanup_stale_temp_files do
    cutoff = System.system_time(:second) - 3600

    case File.ls(Storage.tmp_dir()) do
      {:ok, entries} ->
        Enum.each(entries, fn entry ->
          path = Path.join(Storage.tmp_dir(), entry)

          with {:ok, %{type: :regular, mtime: mtime}} <- File.lstat(path, time: :posix),
               true <- mtime < cutoff do
            File.rm(path)
          else
            _ -> :ok
          end
        end)

      {:error, _reason} ->
        :ok
    end
  end

  defp log_orphan_final_files do
    case File.ls(Storage.final_dir()) do
      {:ok, entries} ->
        Enum.each(entries, fn entry ->
          path = Path.join(Storage.final_dir(), entry)

          if entry != "tmp" and File.regular?(path) and
               not Repo.exists?(
                 from(attachment in Attachment, where: attachment.managed_path == ^path)
               ) do
            Logger.warning("orphan attachment file retained: #{path}")
          end
        end)

      {:error, _reason} ->
        :ok
    end
  end

  defp text_attachment?(%Attachment{media_type: media_type}) do
    String.starts_with?(media_type, "text/") or
      media_type in ["application/json", "application/xml"]
  end

  defp require_managed_regular_file(path) do
    case File.lstat(path) do
      {:ok, %{type: :regular}} -> :ok
      _ -> {:error, storage_error(:unreadable)}
    end
  end

  defp ticket_project_id(ticket_id),
    do:
      Repo.one(from(ticket in Ticket, where: ticket.id == ^ticket_id, select: ticket.project_id))

  defp locked_project_if_present(nil), do: nil

  defp locked_project_if_present(id),
    do: Repo.one(from(project in Project, where: project.id == ^id, lock: "FOR UPDATE"))

  defp locked_ticket(id),
    do: Repo.one(from(ticket in Ticket, where: ticket.id == ^id, lock: "FOR UPDATE"))

  defp require_project(nil), do: {:error, %Error{kind: :not_found, message: "project not found"}}
  defp require_project(project), do: {:ok, project}
  defp require_ticket(nil), do: {:error, %Error{kind: :not_found, message: "ticket not found"}}
  defp require_ticket(ticket), do: {:ok, ticket}

  defp increment_revision(ticket),
    do: ticket |> Ecto.Changeset.change(revision: ticket.revision + 1) |> Repo.update()

  defp cast_uuid(value, field) do
    case Ecto.UUID.cast(value) do
      {:ok, id} -> {:ok, id}
      :error -> invalid_argument(field, "must be a valid UUID")
    end
  end

  defp authorize(%Context{scope: :global, actor: actor}) when actor in [:me, :codex], do: :ok
  defp authorize(_), do: unauthorized()

  defp unauthorized,
    do:
      {:error, %Error{kind: :unauthorized, message: "a global authorization context is required"}}

  defp invalid_argument(field, message),
    do:
      {:error,
       %Error{
         kind: :validation_failed,
         message: "attachment validation failed",
         fields: %{field => [message]}
       }}

  defp storage_error(reason),
    do: invalid_argument(:source_path, storage_message(reason)) |> elem(1)

  defp storage_message(:not_absolute), do: "must be an absolute path"
  defp storage_message(:not_regular), do: "must be a regular non-symlink file"
  defp storage_message(:too_large), do: "exceeds the configured attachment size limit"
  defp storage_message(:source_changed), do: "changed while being copied"
  defp storage_message(_), do: "could not be read"

  defp validation_error(changeset),
    do: %Error{
      kind: :validation_failed,
      message: "attachment validation failed",
      fields: Ecto.Changeset.traverse_errors(changeset, fn {message, _options} -> message end)
    }
end
