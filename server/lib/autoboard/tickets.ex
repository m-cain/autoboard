defmodule Autoboard.Tickets do
  import Ecto.Query

  alias Autoboard.Activity
  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Scope
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Projects.Project
  alias Autoboard.Repo
  alias Autoboard.Tickets.Dependency
  alias Autoboard.Tickets.Graph
  alias Autoboard.Tickets.Label
  alias Autoboard.Tickets.Ticket

  @type result(value) :: {:ok, value} | {:error, Error.t()}

  @spec create(Context.t(), map()) :: result(Ticket.t())
  def create(%Context{} = ctx, attrs) do
    with :ok <- Scope.authorize(ctx),
         {:ok, attrs} <- canonical_attrs(attrs),
         {:ok, project_id} <- cast_uuid(Map.get(attrs, "project_id"), :project_id),
         {:ok, parent_ticket_id} <-
           optional_uuid(Map.get(attrs, "parent_ticket_id"), :parent_ticket_id),
         {:ok, labels} <- normalized_labels(attrs),
         :ok <- valid_changeset(Ticket.create_changeset(%Ticket{}, attrs)) do
      labels = if labels == :not_supplied, do: [], else: labels

      Activity.commit(fn ->
        project = locked_project(ctx, project_id)

        with {:ok, project} <- require_project(project),
             :ok <- Projects.ensure_active(project),
             :ok <- validate_parent(ctx, parent_ticket_id, project.id),
             {:ok, project} <- allocate_ticket_number(project),
             {:ok, ticket} <-
               %Ticket{}
               |> Ticket.create_changeset(attrs)
               |> Ecto.Changeset.put_change(:project_id, project.id)
               |> Ecto.Changeset.put_change(:parent_ticket_id, parent_ticket_id)
               |> Ecto.Changeset.put_change(:number, project.next_ticket_number - 1)
               |> Repo.insert(),
             {:ok, labels} <- resolve_labels(ctx, project.id, labels),
             :ok <- replace_labels(ticket.id, labels),
             ticket = present(ctx, ticket, project, labels),
             {:ok, event} <-
               Activity.append(
                 ctx,
                 "ticket.created",
                 project.id,
                 ticket.id,
                 created_payload(ticket)
               ) do
          {ticket, [event]}
        else
          {:error, %Ecto.Changeset{} = changeset} -> Repo.rollback(validation_error(changeset))
          {:error, %Error{} = error} -> Repo.rollback(error)
        end
      end)
      |> transaction_result()
    end
  end

  def create(_ctx, _attrs), do: unauthorized()

  @spec update(Context.t(), Ecto.UUID.t(), pos_integer(), map()) :: result(Ticket.t())
  def update(%Context{} = ctx, id, expected_revision, attrs) do
    with :ok <- Scope.authorize(ctx),
         {:ok, id} <- cast_uuid(id, :id),
         :ok <- validate_expected_revision(expected_revision),
         {:ok, attrs} <- canonical_attrs(attrs),
         {:ok, labels} <- normalized_labels(attrs),
         :ok <- valid_changeset(Ticket.update_changeset(%Ticket{}, attrs)) do
      Activity.commit(fn ->
        project = id |> ticket_project_id(ctx) |> locked_project_if_present(ctx)
        ticket = locked_ticket(ctx, id)

        with {:ok, ticket} <- require_ticket(ticket),
             {:ok, project} <- require_project(project),
             :ok <- require_revision(ctx, ticket, expected_revision),
             :ok <- Projects.ensure_active(project),
             current_labels = ticket_labels(ctx, ticket),
             labels_changed? = labels_changed?(attrs, labels, current_labels),
             changeset = Ticket.update_changeset(ticket, attrs),
             :ok <- require_ticket_change(changeset, labels_changed?),
             {:ok, updated} <-
               changeset
               |> Ecto.Changeset.put_change(:revision, ticket.revision + 1)
               |> Repo.update(),
             {:ok, updated_labels} <-
               maybe_replace_labels(ctx, updated.id, labels, current_labels, labels_changed?),
             updated = present(ctx, updated, project, updated_labels),
             {:ok, event} <-
               Activity.append(
                 ctx,
                 "ticket.updated",
                 project.id,
                 updated.id,
                 update_payload(
                   ticket,
                   changeset,
                   current_labels,
                   updated_labels,
                   labels_changed?
                 )
               ) do
          {updated, [event]}
        else
          {:error, %Ecto.Changeset{} = changeset} -> Repo.rollback(validation_error(changeset))
          {:error, %Error{} = error} -> Repo.rollback(error)
        end
      end)
      |> transaction_result()
    end
  end

  def update(_ctx, _id, _expected_revision, _attrs), do: unauthorized()

  @spec transition(Context.t(), Ecto.UUID.t(), pos_integer(), atom() | String.t()) ::
          result(Ticket.t())
  def transition(%Context{} = ctx, id, expected_revision, status) do
    with :ok <- Scope.authorize(ctx),
         {:ok, id} <- cast_uuid(id, :id),
         :ok <- validate_expected_revision(expected_revision),
         {:ok, status} <- normalize_status(status) do
      Activity.commit(fn ->
        project = id |> ticket_project_id(ctx) |> locked_project_if_present(ctx)
        ticket = locked_ticket(ctx, id)

        with {:ok, ticket} <- require_ticket(ticket),
             {:ok, project} <- require_project(project),
             :ok <- require_revision(ctx, ticket, expected_revision),
             :ok <- Projects.ensure_active(project),
             :ok <- require_status_change(ticket, status),
             :ok <- transition_allowed(ctx, ticket, status),
             {:ok, updated} <- Repo.update(Ticket.transition_changeset(ticket, status)),
             updated = present(ctx, updated, project, ticket_labels(ctx, ticket)),
             {:ok, transition_event} <-
               Activity.append(ctx, "ticket.transitioned", project.id, updated.id, %{
                 "status" => %{
                   "from" => Atom.to_string(ticket.status),
                   "to" => Atom.to_string(status)
                 }
               }),
             {:ok, blocking_events} <-
               notify_directly_blocked_tickets(ctx, project, ticket, status) do
          {updated, [transition_event | blocking_events]}
        else
          {:error, %Ecto.Changeset{} = changeset} -> Repo.rollback(validation_error(changeset))
          {:error, %Error{} = error} -> Repo.rollback(error)
        end
      end)
      |> transition_transaction_result()
    end
  end

  def transition(_ctx, _id, _expected_revision, _status), do: unauthorized()

  @spec fetch(Context.t(), Ecto.UUID.t()) :: result(Ticket.t())
  def fetch(%Context{} = ctx, id) do
    with {:ok, query} <- Scope.tickets(ctx),
         {:ok, id} <- cast_uuid(id, :id),
         {:ok, ticket} <- require_ticket(Repo.one(where(query, [ticket], ticket.id == ^id))) do
      {:ok, present(ctx, ticket)}
    end
  end

  def fetch(_ctx, _id), do: unauthorized()

  @spec search(Context.t(), map()) :: result([Ticket.t()])
  def search(%Context{} = ctx, attrs) do
    with {:ok, base_query} <- Scope.tickets(ctx),
         {:ok, attrs} <- canonical_search_attrs(attrs),
         {:ok, project_id} <- optional_uuid(Map.get(attrs, "project_id"), :project_id),
         {:ok, query} <- optional_query(Map.get(attrs, "query")) do
      tickets =
        base_query
        |> maybe_filter_project(project_id)
        |> maybe_filter_query(query)
        |> order_by([ticket], asc: ticket.inserted_at)
        |> Repo.all()
        |> Enum.map(&present(ctx, &1))

      {:ok, tickets}
    end
  end

  def search(_ctx, _attrs), do: unauthorized()

  @spec add_dependency(Context.t(), Ecto.UUID.t(), Ecto.UUID.t(), pos_integer()) ::
          result(Ticket.t())
  def add_dependency(%Context{} = ctx, blocked_ticket_id, blocker_ticket_id, expected_revision) do
    with :ok <- Scope.authorize(ctx),
         {:ok, blocked_ticket_id} <- cast_uuid(blocked_ticket_id, :blocked_ticket_id),
         {:ok, blocker_ticket_id} <- cast_uuid(blocker_ticket_id, :blocker_ticket_id),
         :ok <- validate_expected_revision(expected_revision) do
      mutate_dependency(ctx, blocked_ticket_id, blocker_ticket_id, expected_revision, :add)
    end
  end

  def add_dependency(_ctx, _blocked_ticket_id, _blocker_ticket_id, _expected_revision),
    do: unauthorized()

  @spec remove_dependency(Context.t(), Ecto.UUID.t(), Ecto.UUID.t(), pos_integer()) ::
          result(Ticket.t())
  def remove_dependency(%Context{} = ctx, blocked_ticket_id, blocker_ticket_id, expected_revision) do
    with :ok <- Scope.authorize(ctx),
         {:ok, blocked_ticket_id} <- cast_uuid(blocked_ticket_id, :blocked_ticket_id),
         {:ok, blocker_ticket_id} <- cast_uuid(blocker_ticket_id, :blocker_ticket_id),
         :ok <- validate_expected_revision(expected_revision) do
      mutate_dependency(ctx, blocked_ticket_id, blocker_ticket_id, expected_revision, :remove)
    end
  end

  def remove_dependency(_ctx, _blocked_ticket_id, _blocker_ticket_id, _expected_revision),
    do: unauthorized()

  @spec blocked?(Context.t(), Ticket.t()) :: boolean() | {:error, Error.t()}
  def blocked?(%Context{} = ctx, %Ticket{} = ticket) do
    with {:ok, query} <- Scope.tickets(ctx),
         %Ticket{} <- Repo.one(where(query, [candidate], candidate.id == ^ticket.id)) do
      unresolved_blocker?(ctx, ticket.id)
    else
      nil -> {:error, %Error{kind: :not_found, message: "ticket not found"}}
      {:error, %Error{} = error} -> {:error, error}
    end
  end

  def blocked?(_ctx, _ticket), do: unauthorized()

  @spec list_actionable(Context.t(), map()) :: [Ticket.t()] | {:error, Error.t()}
  def list_actionable(%Context{} = ctx, attrs) do
    with {:ok, base_query} <- Scope.tickets(ctx, from(ticket in Ticket, as: :ticket)),
         {:ok, attrs} <- canonical_actionable_attrs(attrs),
         {:ok, project_id} <- optional_uuid(Map.get(attrs, "project_id"), :project_id),
         {:ok, limit} <- actionable_limit(Map.get(attrs, "limit")) do
      tickets =
        base_query
        |> join(:inner, [ticket], project in Project, on: project.id == ticket.project_id)
        |> where(
          [ticket, project],
          ticket.status == :ready and ticket.assignee == :codex and project.state == :active
        )
        |> maybe_filter_project(project_id)
        |> without_unresolved_blockers(ctx)
        |> without_non_terminal_subtasks(ctx)
        |> order_by([ticket],
          asc:
            fragment(
              "CASE ? WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END",
              ticket.priority
            ),
          asc: ticket.inserted_at,
          asc: ticket.id
        )
        |> limit(^limit)
        |> Repo.all()
        |> Enum.map(&present(ctx, &1))

      tickets
    end
  end

  def list_actionable(_ctx, _attrs), do: unauthorized()

  defp mutate_dependency(ctx, blocked_ticket_id, blocker_ticket_id, expected_revision, operation) do
    Activity.commit(fn ->
      project = blocked_ticket_id |> ticket_project_id(ctx) |> locked_project_if_present(ctx)
      blocked_ticket = locked_ticket(ctx, blocked_ticket_id)

      with {:ok, blocked_ticket} <- require_ticket(blocked_ticket),
           {:ok, project} <- require_project(project),
           :ok <- require_revision(ctx, blocked_ticket, expected_revision),
           :ok <- Projects.ensure_active(project),
           {:ok, blocker_ticket} <- scoped_ticket(ctx, blocker_ticket_id),
           :ok <- require_same_project(blocked_ticket, blocker_ticket),
           :ok <-
             mutate_dependency_edge(ctx, operation, blocked_ticket, blocker_ticket, project.id),
           {:ok, updated} <- increment_revision(blocked_ticket),
           {:ok, event} <-
             Activity.append(
               ctx,
               dependency_event_type(operation),
               project.id,
               updated.id,
               %{"blocker_ticket_id" => blocker_ticket.id}
             ) do
        {present(ctx, updated, project, ticket_labels(ctx, updated)), [event]}
      else
        {:error, %Ecto.Changeset{} = changeset} -> Repo.rollback(validation_error(changeset))
        {:error, %Error{} = error} -> Repo.rollback(error)
      end
    end)
    |> dependency_transaction_result()
  end

  defp mutate_dependency_edge(ctx, :add, blocked_ticket, blocker_ticket, project_id) do
    with :ok <- reject_self_dependency(blocked_ticket, blocker_ticket),
         :ok <- reject_duplicate_dependency(blocked_ticket, blocker_ticket),
         :ok <- reject_dependency_cycle(ctx, blocked_ticket, blocker_ticket, project_id),
         {:ok, _dependency} <-
           %Dependency{}
           |> Dependency.changeset(%{
             blocker_ticket_id: blocker_ticket.id,
             blocked_ticket_id: blocked_ticket.id
           })
           |> Repo.insert() do
      :ok
    end
  end

  defp mutate_dependency_edge(_ctx, :remove, blocked_ticket, blocker_ticket, _project_id) do
    case Repo.get_by(Dependency,
           blocker_ticket_id: blocker_ticket.id,
           blocked_ticket_id: blocked_ticket.id
         ) do
      nil -> invalid_argument(:blocker_ticket_id, "must reference an existing dependency")
      dependency -> Repo.delete(dependency) |> delete_dependency_result()
    end
  end

  defp delete_dependency_result({:ok, _dependency}), do: :ok
  defp delete_dependency_result({:error, changeset}), do: {:error, changeset}

  defp reject_self_dependency(%Ticket{id: id}, %Ticket{id: id}),
    do: invalid_argument(:blocker_ticket_id, "must not equal blocked_ticket_id")

  defp reject_self_dependency(_blocked_ticket, _blocker_ticket), do: :ok

  defp require_same_project(%Ticket{project_id: project_id}, %Ticket{project_id: project_id}),
    do: :ok

  defp require_same_project(_blocked_ticket, _blocker_ticket),
    do: invalid_argument(:blocker_ticket_id, "must belong to the same project")

  defp reject_duplicate_dependency(blocked_ticket, blocker_ticket) do
    if Repo.exists?(
         from(dependency in Dependency,
           where:
             dependency.blocked_ticket_id == ^blocked_ticket.id and
               dependency.blocker_ticket_id == ^blocker_ticket.id
         )
       ) do
      invalid_argument(:blocker_ticket_id, "already blocks this ticket")
    else
      :ok
    end
  end

  defp reject_dependency_cycle(ctx, blocked_ticket, blocker_ticket, project_id) do
    edges = project_dependency_edges(ctx, project_id)

    if Graph.reachable?(edges, blocked_ticket.id, blocker_ticket.id) do
      {:error,
       %Error{
         kind: :dependency_cycle,
         message: "dependency would create a cycle",
         fields: %{blocker_ticket_id: ["must not create a cycle"]}
       }}
    else
      :ok
    end
  end

  defp project_dependency_edges(ctx, project_id) do
    {:ok, tickets} = Scope.tickets(ctx)

    Repo.all(
      from(blocked_ticket in tickets,
        join: dependency in Dependency,
        on: blocked_ticket.id == dependency.blocked_ticket_id,
        where: blocked_ticket.project_id == ^project_id,
        select: {dependency.blocker_ticket_id, dependency.blocked_ticket_id}
      )
    )
  end

  defp dependency_event_type(:add), do: "dependency.added"
  defp dependency_event_type(:remove), do: "dependency.removed"

  defp increment_revision(ticket) do
    ticket
    |> Ecto.Changeset.change(revision: ticket.revision + 1)
    |> Repo.update()
  end

  defp unresolved_blocker?(ctx, ticket_id) do
    {:ok, blockers} = Scope.tickets(ctx)

    Repo.exists?(
      from(blocker in blockers,
        join: dependency in Dependency,
        on: dependency.blocker_ticket_id == blocker.id,
        where:
          dependency.blocked_ticket_id == ^ticket_id and blocker.status not in [:done, :canceled]
      )
    )
  end

  defp without_unresolved_blockers(query, ctx) do
    {:ok, blocker_scope} = Scope.tickets(ctx)

    blockers =
      from(blocker in blocker_scope,
        join: dependency in Dependency,
        on: dependency.blocker_ticket_id == blocker.id,
        where:
          dependency.blocked_ticket_id == parent_as(:ticket).id and
            blocker.status not in [:done, :canceled],
        select: 1
      )

    where(query, [_ticket], not exists(blockers))
  end

  defp without_non_terminal_subtasks(query, ctx) do
    {:ok, ticket_scope} = Scope.tickets(ctx)

    subtasks =
      from(subtask in ticket_scope,
        where:
          subtask.parent_ticket_id == parent_as(:ticket).id and
            subtask.status not in [:done, :canceled],
        select: 1
      )

    where(query, [_ticket], not exists(subtasks))
  end

  defp notify_directly_blocked_tickets(ctx, project, ticket, status) do
    if terminal?(ticket.status) != terminal?(status) do
      ticket
      |> directly_blocked_tickets(ctx)
      |> Enum.uniq_by(& &1.id)
      |> Enum.reduce_while({:ok, []}, fn blocked_ticket, {:ok, events} ->
        with {:ok, updated} <- increment_revision(blocked_ticket),
             {:ok, event} <-
               Activity.append(
                 ctx,
                 "dependency.blocking_changed",
                 project.id,
                 updated.id,
                 %{
                   "blocker_ticket_id" => ticket.id,
                   "status" => %{
                     "from" => Atom.to_string(ticket.status),
                     "to" => Atom.to_string(status)
                   }
                 }
               ) do
          {:cont, {:ok, [event | events]}}
        else
          {:error, error} -> {:halt, {:error, error}}
        end
      end)
      |> case do
        {:ok, events} -> {:ok, Enum.reverse(events)}
        {:error, error} -> {:error, error}
      end
    else
      {:ok, []}
    end
  end

  defp directly_blocked_tickets(ticket, ctx) do
    {:ok, tickets} = Scope.tickets(ctx)

    Repo.all(
      from(blocked_ticket in tickets,
        join: dependency in Dependency,
        on: dependency.blocked_ticket_id == blocked_ticket.id,
        where: dependency.blocker_ticket_id == ^ticket.id,
        lock: "FOR UPDATE",
        select: blocked_ticket
      )
    )
  end

  defp terminal?(status), do: status in [:done, :canceled]

  defp allocate_ticket_number(project) do
    project
    |> Ecto.Changeset.change(next_ticket_number: project.next_ticket_number + 1)
    |> Repo.update()
  end

  defp validate_parent(_ctx, nil, _project_id), do: :ok

  defp validate_parent(ctx, parent_ticket_id, project_id) do
    case scoped_ticket(ctx, parent_ticket_id) do
      {:error, %Error{kind: :not_found}} ->
        invalid_argument(:parent_ticket_id, "must reference an existing ticket")

      {:ok, %Ticket{project_id: parent_project_id}} when parent_project_id != project_id ->
        invalid_argument(:parent_ticket_id, "must belong to the same project")

      {:ok, %Ticket{parent_ticket_id: parent_id}} when not is_nil(parent_id) ->
        invalid_argument(:parent_ticket_id, "must not create a grandchild")

      {:ok, %Ticket{}} ->
        :ok

      {:error, %Error{} = error} ->
        {:error, error}
    end
  end

  defp normalized_labels(attrs) do
    case Map.fetch(attrs, "labels") do
      :error -> {:ok, :not_supplied}
      {:ok, labels} when is_list(labels) -> normalize_label_list(labels)
      {:ok, _labels} -> invalid_argument(:labels, "must be a list of strings")
    end
  end

  defp normalize_label_list(labels) do
    labels
    |> Enum.reduce_while({:ok, %{}}, fn label, {:ok, normalized} ->
      with true <- is_binary(label),
           name <- label |> String.trim() |> String.replace(~r/\s+/, " "),
           true <- name != "",
           true <- String.length(name) <= 50 do
        {:cont, {:ok, Map.put_new(normalized, String.downcase(name), name)}}
      else
        false ->
          {:halt, invalid_argument(:labels, "entries must be trimmed strings of 1-50 characters")}
      end
    end)
    |> case do
      {:ok, normalized} when map_size(normalized) <= 20 ->
        {:ok, normalized |> Map.values() |> Enum.sort_by(&String.downcase/1)}

      {:ok, _normalized} ->
        invalid_argument(:labels, "must contain at most 20 labels")

      error ->
        error
    end
  end

  defp resolve_labels(_ctx, _project_id, :not_supplied), do: {:ok, :not_supplied}

  defp resolve_labels(ctx, project_id, names) do
    {:ok, label_scope} = Scope.labels(ctx)

    labels =
      Enum.map(names, fn name ->
        changeset = Label.changeset(%Label{}, %{project_id: project_id, name: name})

        with {:ok, _label} <-
               Repo.insert(changeset,
                 on_conflict: :nothing,
                 conflict_target: [:project_id, :name]
               ),
             %Label{} = label <-
               Repo.one(
                 from(label in label_scope,
                   where:
                     label.project_id == ^project_id and
                       fragment("lower(?)", label.name) == ^String.downcase(name)
                 )
               ) do
          {:ok, label}
        else
          {:error, changeset} -> {:error, changeset}
          nil -> {:error, %Error{kind: :internal_error, message: "label lookup failed"}}
        end
      end)

    case Enum.find(labels, &match?({:error, _}, &1)) do
      nil -> {:ok, Enum.map(labels, fn {:ok, label} -> label end)}
      {:error, error} -> {:error, error}
    end
  end

  defp ticket_labels(ctx, ticket) do
    {:ok, labels} = Scope.labels(ctx)
    ticket |> Repo.preload(labels: labels) |> Map.fetch!(:labels)
  end

  defp labels_changed?(_attrs, :not_supplied, _current_labels), do: false

  defp labels_changed?(_attrs, desired_labels, current_labels) do
    desired_labels
    |> Enum.map(&String.downcase/1)
    |> MapSet.new()
    |> Kernel.!=(current_labels |> Enum.map(&String.downcase(&1.name)) |> MapSet.new())
  end

  defp replace_labels(_ticket_id, :not_supplied), do: :ok

  defp replace_labels(ticket_id, labels) do
    Repo.delete_all(
      from(ticket_label in "ticket_labels",
        where: field(ticket_label, :ticket_id) == type(^ticket_id, :binary_id)
      )
    )

    {:ok, ticket_id} = Ecto.UUID.dump(ticket_id)

    Repo.insert_all(
      "ticket_labels",
      Enum.map(labels, fn label ->
        {:ok, label_id} = Ecto.UUID.dump(label.id)
        %{ticket_id: ticket_id, label_id: label_id}
      end)
    )

    :ok
  end

  defp maybe_replace_labels(_ctx, _ticket_id, :not_supplied, current_labels, false),
    do: {:ok, current_labels}

  defp maybe_replace_labels(_ctx, _ticket_id, _labels, current_labels, false),
    do: {:ok, current_labels}

  defp maybe_replace_labels(ctx, ticket_id, labels, _current_labels, true) do
    with {:ok, labels} <- resolve_labels_from_names(ctx, ticket_id, labels),
         :ok <- replace_labels(ticket_id, labels) do
      {:ok, labels}
    end
  end

  defp resolve_labels_from_names(ctx, ticket_id, names) do
    {:ok, tickets} = Scope.tickets(ctx)

    project_id =
      Repo.one!(
        from(ticket in tickets, where: ticket.id == ^ticket_id, select: ticket.project_id)
      )

    resolve_labels(ctx, project_id, names)
  end

  defp require_ticket_change(changeset, labels_changed?) do
    if changeset.valid? and (changeset.changes != %{} or labels_changed?) do
      :ok
    else
      invalid_argument(:base, "must change at least one field")
    end
  end

  defp require_status_change(%Ticket{status: status}, status),
    do: invalid_argument(:status, "must change the status")

  defp require_status_change(_ticket, _status), do: :ok

  defp transition_allowed(ctx, ticket, status) when status in [:done, :canceled] do
    cond do
      status == :done and blocked?(ctx, ticket) ->
        {:error, %Error{kind: :blocked_by_dependency, message: "ticket has unresolved blockers"}}

      has_non_terminal_subtask?(ctx, ticket) ->
        {:error, %Error{kind: :invalid_transition, message: "ticket has non-terminal subtasks"}}

      true ->
        :ok
    end
  end

  defp transition_allowed(_ctx, _ticket, _status), do: :ok

  defp has_non_terminal_subtask?(ctx, ticket) do
    {:ok, tickets} = Scope.tickets(ctx)

    Repo.exists?(
      from(subtask in tickets,
        where:
          subtask.parent_ticket_id == ^ticket.id and
            subtask.status not in [:done, :canceled]
      )
    )
  end

  defp present(ctx, ticket, project \\ nil, labels \\ nil) do
    ticket =
      if is_nil(labels),
        do: %{ticket | labels: ticket_labels(ctx, ticket)},
        else: %{ticket | labels: labels}

    project = project || scoped_project!(ctx, ticket.project_id)

    %{ticket | project: project, identifier: "#{project.key}-#{ticket.number}"}
  end

  defp created_payload(ticket) do
    %{
      "identifier" => ticket.identifier,
      "title" => ticket.title,
      "status" => Atom.to_string(ticket.status),
      "priority" => Atom.to_string(ticket.priority),
      "assignee" => Atom.to_string(ticket.assignee),
      "parent_ticket_id" => ticket.parent_ticket_id,
      "labels" => Enum.map(ticket.labels, & &1.name)
    }
  end

  defp update_payload(ticket, changeset, old_labels, new_labels, labels_changed?) do
    changeset.changes
    |> Map.take([:title, :description, :priority, :assignee])
    |> Map.new(fn {field, value} ->
      {Atom.to_string(field),
       %{"from" => json_value(Map.fetch!(ticket, field)), "to" => json_value(value)}}
    end)
    |> maybe_add_label_payload(old_labels, new_labels, labels_changed?)
  end

  defp maybe_add_label_payload(payload, _old_labels, _new_labels, false), do: payload

  defp maybe_add_label_payload(payload, old_labels, new_labels, true) do
    Map.put(payload, "labels", %{
      "from" => Enum.map(old_labels, & &1.name),
      "to" => Enum.map(new_labels, & &1.name)
    })
  end

  defp json_value(value) when is_atom(value), do: Atom.to_string(value)
  defp json_value(value), do: value

  defp transaction_result({:ok, ticket}), do: {:ok, ticket}
  defp transaction_result({:error, %Error{} = error}), do: {:error, error}

  defp transaction_result({:error, %Ecto.Changeset{} = changeset}),
    do: {:error, validation_error(changeset)}

  defp dependency_transaction_result({:ok, ticket}), do: {:ok, ticket}
  defp dependency_transaction_result({:error, %Error{} = error}), do: {:error, error}

  defp dependency_transaction_result({:error, %Ecto.Changeset{} = changeset}),
    do: {:error, validation_error(changeset)}

  defp transition_transaction_result({:ok, ticket}), do: {:ok, ticket}
  defp transition_transaction_result({:error, %Error{} = error}), do: {:error, error}

  defp transition_transaction_result({:error, %Ecto.Changeset{} = changeset}),
    do: {:error, validation_error(changeset)}

  defp canonical_attrs(attrs) do
    case Ticket.canonicalize_attrs(attrs) do
      {:ok, _attrs, [_ | _] = errors} ->
        {:error,
         %Error{
           kind: :validation_failed,
           message: "ticket validation failed",
           fields: errors_by_pairs(errors)
         }}

      {:ok, attrs, []} ->
        {:ok, attrs}

      :error ->
        invalid_argument(:attrs, "must be a map")
    end
  end

  defp canonical_search_attrs(attrs) do
    with {:ok, attrs} <- canonical_attrs(attrs) do
      unsupported = Map.keys(attrs) -- ["project_id", "query"]

      if unsupported == [] do
        {:ok, attrs}
      else
        invalid_argument(:base, "#{inspect(hd(unsupported))} is not allowed")
      end
    end
  end

  defp canonical_actionable_attrs(attrs) do
    with {:ok, attrs} <- canonical_attrs(attrs) do
      unsupported = Map.keys(attrs) -- ["project_id", "limit"]

      if unsupported == [] do
        {:ok, attrs}
      else
        invalid_argument(:base, "#{inspect(hd(unsupported))} is not allowed")
      end
    end
  end

  defp valid_changeset(%{valid?: true}), do: :ok
  defp valid_changeset(changeset), do: {:error, validation_error(changeset)}

  defp optional_uuid(nil, _field), do: {:ok, nil}
  defp optional_uuid(value, field), do: cast_uuid(value, field)

  defp cast_uuid(value, field) do
    case Ecto.UUID.cast(value) do
      {:ok, id} -> {:ok, id}
      :error -> invalid_argument(field, "must be a valid UUID")
    end
  end

  defp optional_query(nil), do: {:ok, nil}
  defp optional_query(value) when is_binary(value), do: {:ok, value}
  defp optional_query(_value), do: invalid_argument(:query, "must be a string")

  defp actionable_limit(nil), do: {:ok, 25}
  defp actionable_limit(limit) when is_integer(limit) and limit in 1..100, do: {:ok, limit}
  defp actionable_limit(_limit), do: invalid_argument(:limit, "must be an integer from 1 to 100")

  defp normalize_status(status)
       when status in [:triage, :backlog, :ready, :in_progress, :done, :canceled],
       do: {:ok, status}

  defp normalize_status(status) when is_binary(status) do
    case Enum.find(Ticket.statuses(), &(Atom.to_string(&1) == status)) do
      nil -> invalid_argument(:status, "must be a supported status")
      status -> {:ok, status}
    end
  end

  defp normalize_status(_status), do: invalid_argument(:status, "must be a supported status")

  defp maybe_filter_project(query, nil), do: query

  defp maybe_filter_project(query, project_id),
    do: where(query, [ticket], ticket.project_id == ^project_id)

  defp maybe_filter_query(query, nil), do: query

  defp maybe_filter_query(query, text) do
    where(query, [ticket], ilike(ticket.title, ^"%#{text}%"))
  end

  defp locked_project(ctx, id) do
    {:ok, projects} = Scope.projects(ctx)
    Repo.one(from(project in projects, where: project.id == ^id, lock: "FOR UPDATE"))
  end

  defp locked_project_if_present(nil, _ctx), do: nil
  defp locked_project_if_present(project_id, ctx), do: locked_project(ctx, project_id)

  defp locked_ticket(ctx, id) do
    {:ok, tickets} = Scope.tickets(ctx)
    Repo.one(from(ticket in tickets, where: ticket.id == ^id, lock: "FOR UPDATE"))
  end

  defp ticket_project_id(ticket_id, ctx) do
    {:ok, tickets} = Scope.tickets(ctx)
    Repo.one(from(ticket in tickets, where: ticket.id == ^ticket_id, select: ticket.project_id))
  end

  defp scoped_ticket(ctx, ticket_id) do
    {:ok, tickets} = Scope.tickets(ctx)
    tickets |> where([ticket], ticket.id == ^ticket_id) |> Repo.one() |> require_ticket()
  end

  defp scoped_project!(ctx, project_id) do
    {:ok, projects} = Scope.projects(ctx)
    Repo.one!(from(project in projects, where: project.id == ^project_id))
  end

  defp require_project(nil), do: {:error, %Error{kind: :not_found, message: "project not found"}}
  defp require_project(project), do: {:ok, project}
  defp require_ticket(nil), do: {:error, %Error{kind: :not_found, message: "ticket not found"}}
  defp require_ticket(ticket), do: {:ok, ticket}

  defp require_revision(_ctx, %Ticket{revision: revision}, revision), do: :ok

  defp require_revision(ctx, ticket, _expected_revision) do
    {:error,
     %Error{
       kind: :revision_conflict,
       message: "ticket has changed",
       current: present(ctx, ticket)
     }}
  end

  defp validate_expected_revision(revision) when is_integer(revision) and revision > 0, do: :ok

  defp validate_expected_revision(_revision),
    do: invalid_argument(:expected_revision, "must be a positive integer")

  defp unauthorized, do: Scope.unauthorized("a global authorization context is required")

  defp invalid_argument(field, message) do
    {:error,
     %Error{
       kind: :validation_failed,
       message: "ticket validation failed",
       fields: %{field => [message]}
     }}
  end

  defp validation_error(changeset) do
    %Error{
      kind: :validation_failed,
      message: "ticket validation failed",
      fields: errors_by_field(changeset)
    }
  end

  defp errors_by_pairs(errors), do: Enum.group_by(errors, &elem(&1, 0), &elem(&1, 1))

  defp errors_by_field(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, options} ->
      Enum.reduce(options, message, fn {key, value}, acc ->
        String.replace(acc, "%{#{key}}", inspect(value))
      end)
    end)
  end
end
