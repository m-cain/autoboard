defmodule Autoboard.Activity do
  import Ecto.Query

  require Logger

  alias Autoboard.Activity.Broadcaster
  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Repo

  @registry Autoboard.Activity.Registry
  @advisory_lock 4_187_043
  @default_replay_page_size 100

  @spec append(Context.t(), String.t(), Ecto.UUID.t(), Ecto.UUID.t() | nil, map()) ::
          {:ok, Event.t()} | {:error, Ecto.Changeset.t()}
  def append(%Context{} = ctx, event_type, project_id, ticket_id, payload)
      when is_binary(event_type) and is_binary(project_id) and
             (is_binary(ticket_id) or is_nil(ticket_id)) and
             is_map(payload) do
    with :ok <- acquire_activity_lock() do
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

  @spec commit((-> {term(), [Event.t()]})) :: {:ok, term()} | {:error, term()}
  def commit(fun) when is_function(fun, 0) do
    if Repo.in_transaction?() do
      {:error, :nested_transaction}
    else
      case Repo.transaction(fun) do
        {:ok, {result, events}} when is_list(events) ->
          Enum.each(events, &safe_broadcast/1)
          {:ok, result}

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  @spec subscribe() :: :ok | {:error, {:already_registered, pid()}}
  def subscribe do
    case Registry.register(@registry, :activity, nil) do
      {:ok, _pid} -> :ok
      error -> error
    end
  end

  @spec unsubscribe() :: :ok
  def unsubscribe, do: Registry.unregister(@registry, :activity)

  @spec broadcast(Event.t()) :: :ok
  def broadcast(%Event{} = event), do: Broadcaster.broadcast(event)

  defp safe_broadcast(event) do
    broadcaster =
      case Application.get_env(:autoboard, :activity_broadcast) do
        fun when is_function(fun, 1) -> fun
        _ -> &broadcast/1
      end

    try do
      broadcaster.(event)
    rescue
      error ->
        Logger.warning("activity broadcast failed after commit: #{Exception.message(error)}")
    catch
      kind, reason ->
        Logger.warning("activity broadcast #{kind} after commit: #{inspect(reason)}")
    end

    :ok
  end

  @spec replay_after(Context.t(), non_neg_integer()) ::
          {:ok, [Event.t()]} | {:error, :unauthorized}
  def replay_after(%Context{scope: :global, actor: actor}, activity_id)
      when actor in [:me, :codex] and is_integer(activity_id) and activity_id >= 0 do
    {:ok,
     Repo.all(from(event in Event, where: event.id > ^activity_id, order_by: [asc: event.id]))}
  end

  def replay_after(_ctx, _activity_id), do: {:error, :unauthorized}

  @spec high_water(Context.t()) :: {:ok, non_neg_integer()} | {:error, :unauthorized}
  def high_water(%Context{scope: :global, actor: actor}) when actor in [:me, :codex] do
    {:ok, Repo.aggregate(Event, :max, :id) || 0}
  end

  def high_water(_ctx), do: {:error, :unauthorized}

  @spec replay_between(Context.t(), non_neg_integer(), non_neg_integer()) ::
          {:ok, [Event.t()]} | {:error, :unauthorized}
  def replay_between(%Context{scope: :global, actor: actor}, lower, upper)
      when actor in [:me, :codex] and is_integer(lower) and lower >= 0 and is_integer(upper) and
             upper >= lower do
    {:ok,
     Repo.all(
       from(event in Event,
         where: event.id > ^lower and event.id <= ^upper,
         order_by: [asc: event.id]
       )
     )}
  end

  def replay_between(_ctx, _lower, _upper), do: {:error, :unauthorized}

  @spec replay_page(Context.t(), non_neg_integer(), non_neg_integer() | nil, pos_integer()) ::
          {:ok, [Event.t()]} | {:error, :unauthorized | :invalid_replay_limit}
  def replay_page(%Context{scope: :global, actor: actor}, lower, upper, limit)
      when actor in [:me, :codex] and is_integer(lower) and lower >= 0 and
             (is_nil(upper) or (is_integer(upper) and upper >= lower)) and is_integer(limit) and
             limit in 1..1000 do
    query =
      from(event in Event,
        where: event.id > ^lower,
        order_by: [asc: event.id],
        limit: ^limit
      )

    query = if is_nil(upper), do: query, else: where(query, [event], event.id <= ^upper)
    {:ok, Repo.all(query)}
  end

  def replay_page(%Context{scope: :global, actor: actor}, _lower, _upper, _limit)
      when actor in [:me, :codex],
      do: {:error, :invalid_replay_limit}

  def replay_page(_ctx, _lower, _upper, _limit), do: {:error, :unauthorized}

  @spec replay_page_size() :: pos_integer()
  def replay_page_size do
    case Application.get_env(:autoboard, :sse_replay_page_size, @default_replay_page_size) do
      size when is_integer(size) and size in 1..1000 -> size
      _ -> @default_replay_page_size
    end
  end

  defp acquire_activity_lock do
    if Repo.in_transaction?() do
      case Repo.query("SELECT pg_advisory_xact_lock($1)", [@advisory_lock]) do
        {:ok, _result} -> :ok
        {:error, reason} -> {:error, reason}
      end
    else
      {:error, :activity_append_requires_transaction}
    end
  end
end
