defmodule Autoboard.Auth.Scope do
  @moduledoc """
  Central authorization and query-boundary policy for canonical project data.

  V1 tokens still authenticate to global contexts. Project contexts exist as an
  internal capability so every domain operation already has one non-bypassable
  place to apply future project-scoped credentials.
  """

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Attachments.Attachment
  alias Autoboard.Auth.Context
  alias Autoboard.Comments.Comment
  alias Autoboard.Domain.Error
  alias Autoboard.Projects.Project
  alias Autoboard.Tickets.Label
  alias Autoboard.Tickets.Ticket

  @spec authorize(Context.t()) :: :ok | {:error, Error.t()}
  def authorize(%Context{actor: actor, scope: :global}) when actor in [:me, :codex], do: :ok

  def authorize(%Context{actor: actor, scope: {:project, project_id}})
      when actor in [:me, :codex] and is_binary(project_id) do
    case Ecto.UUID.cast(project_id) do
      {:ok, ^project_id} -> :ok
      _ -> unauthorized()
    end
  end

  def authorize(_ctx), do: unauthorized()

  @spec authorize_global(Context.t()) :: :ok | {:error, Error.t()}
  def authorize_global(%Context{actor: actor, scope: :global}) when actor in [:me, :codex],
    do: :ok

  def authorize_global(_ctx), do: unauthorized("a global authorization context is required")

  @spec projects(Context.t(), Ecto.Queryable.t()) :: {:ok, Ecto.Query.t()} | {:error, Error.t()}
  def projects(ctx, queryable \\ Project) do
    with :ok <- authorize(ctx) do
      query = from(project in queryable)

      {:ok,
       case ctx.scope do
         :global -> query
         {:project, project_id} -> where(query, [project], project.id == ^project_id)
       end}
    end
  end

  @spec tickets(Context.t(), Ecto.Queryable.t()) :: {:ok, Ecto.Query.t()} | {:error, Error.t()}
  def tickets(ctx, queryable \\ Ticket), do: project_records(ctx, queryable)

  @spec comments(Context.t(), Ecto.Queryable.t()) :: {:ok, Ecto.Query.t()} | {:error, Error.t()}
  def comments(ctx, queryable \\ Comment), do: project_records(ctx, queryable)

  @spec attachments(Context.t(), Ecto.Queryable.t()) ::
          {:ok, Ecto.Query.t()} | {:error, Error.t()}
  def attachments(ctx, queryable \\ Attachment), do: project_records(ctx, queryable)

  @spec labels(Context.t(), Ecto.Queryable.t()) :: {:ok, Ecto.Query.t()} | {:error, Error.t()}
  def labels(ctx, queryable \\ Label), do: project_records(ctx, queryable)

  @spec events(Context.t(), Ecto.Queryable.t()) :: {:ok, Ecto.Query.t()} | {:error, Error.t()}
  def events(ctx, queryable \\ Event), do: project_records(ctx, queryable)

  @spec project_id(Context.t()) :: :global | {:project, Ecto.UUID.t()} | {:error, Error.t()}
  def project_id(%Context{actor: actor, scope: :global}) when actor in [:me, :codex], do: :global

  def project_id(%Context{} = ctx) do
    with :ok <- authorize(ctx), {:project, project_id} <- ctx.scope, do: {:project, project_id}
  end

  def project_id(_ctx), do: unauthorized()

  @spec unauthorized(String.t()) :: {:error, Error.t()}
  def unauthorized(message \\ "an authorization context is required") do
    {:error, %Error{kind: :unauthorized, message: message}}
  end

  defp project_records(ctx, queryable) do
    with :ok <- authorize(ctx) do
      query = from(record in queryable)

      {:ok,
       case ctx.scope do
         :global -> query
         {:project, project_id} -> where(query, [record], record.project_id == ^project_id)
       end}
    end
  end
end
