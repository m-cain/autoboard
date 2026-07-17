defmodule Autoboard.Tickets.Ticket do
  use Ecto.Schema

  import Ecto.Changeset

  alias Autoboard.Projects.Project
  alias Autoboard.Tickets.Label

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  @statuses [:triage, :backlog, :ready, :in_progress, :done, :canceled]
  @priorities [:none, :low, :medium, :high, :urgent]
  @assignees [:unassigned, :me, :codex]

  schema "tickets" do
    field(:number, :integer)
    field(:identifier, :string, virtual: true)
    field(:title, :string)
    field(:description, :string, default: "")
    field(:status, Ecto.Enum, values: @statuses, default: :triage)
    field(:priority, Ecto.Enum, values: @priorities, default: :none)
    field(:assignee, Ecto.Enum, values: @assignees, default: :unassigned)
    field(:revision, :integer, default: 1)
    belongs_to(:project, Project)
    belongs_to(:parent_ticket, __MODULE__)
    has_many(:subtasks, __MODULE__, foreign_key: :parent_ticket_id)
    many_to_many(:labels, Label, join_through: "ticket_labels")

    timestamps(type: :utc_datetime_usec)
  end

  @type t :: %__MODULE__{}
  @spec statuses() :: [atom()]
  def statuses, do: @statuses
  @spec priorities() :: [atom()]
  def priorities, do: @priorities
  @spec assignees() :: [atom()]
  def assignees, do: @assignees

  def create_changeset(ticket, attrs) do
    {attrs, attribute_errors} = canonical_attrs_or_error(attrs)

    ticket
    |> cast(attrs, [:title, :description, :status, :priority, :assignee])
    |> add_attribute_errors(attribute_errors)
    |> reject_unsupported_fields(attrs, ["project_id", "parent_ticket_id", "labels"])
    |> validate_text_types(attrs, [:title, :description])
    |> validate_required([:title])
    |> validate_title()
  end

  def update_changeset(ticket, attrs) do
    {attrs, attribute_errors} = canonical_attrs_or_error(attrs)

    ticket
    |> cast(attrs, [:title, :description, :priority, :assignee])
    |> add_attribute_errors(attribute_errors)
    |> reject_unsupported_fields(attrs, ["labels"])
    |> validate_text_types(attrs, [:title, :description])
    |> validate_title()
  end

  def transition_changeset(ticket, status) when status in @statuses do
    change(ticket, status: status, revision: ticket.revision + 1)
  end

  def canonicalize_attrs(attrs) when is_map(attrs) do
    {attrs, errors} =
      Enum.reduce(attrs, {%{}, []}, fn {key, value}, {attrs, errors} ->
        case canonicalize_key(key) do
          {:ok, key} ->
            if Map.has_key?(attrs, key) do
              {attrs, [{:base, "duplicate attribute #{inspect(key)}"} | errors]}
            else
              {Map.put(attrs, key, value), errors}
            end

          :error ->
            {attrs, [{:base, "attribute key #{inspect(key)} must be an atom or string"} | errors]}
        end
      end)

    {:ok, attrs, Enum.reverse(errors)}
  end

  def canonicalize_attrs(_attrs), do: :error

  defp canonical_attrs_or_error(attrs) do
    case canonicalize_attrs(attrs) do
      {:ok, attrs, errors} -> {attrs, errors}
      :error -> {%{}, [base: "attrs must be a map"]}
    end
  end

  defp canonicalize_key(key) when is_atom(key), do: {:ok, Atom.to_string(key)}
  defp canonicalize_key(key) when is_binary(key), do: {:ok, key}
  defp canonicalize_key(_key), do: :error

  defp add_attribute_errors(changeset, errors) do
    Enum.reduce(errors, changeset, fn {field, message}, changeset ->
      add_error(changeset, field, message)
    end)
  end

  defp reject_unsupported_fields(changeset, attrs, additional_allowed) do
    allowed = ["title", "description", "status", "priority", "assignee"] ++ additional_allowed

    Enum.reduce(attrs, changeset, fn {field, _value}, changeset ->
      if field in allowed,
        do: changeset,
        else: add_error(changeset, :base, "#{inspect(field)} is not allowed")
    end)
  end

  defp validate_text_types(changeset, attrs, fields) do
    Enum.reduce(fields, changeset, fn field, changeset ->
      case Map.fetch(attrs, Atom.to_string(field)) do
        {:ok, value} when is_binary(value) -> changeset
        {:ok, _value} -> add_error(changeset, field, "must be a string")
        :error -> changeset
      end
    end)
  end

  defp validate_title(changeset) do
    changeset
    |> validate_change(:title, fn :title, title ->
      trimmed = if is_binary(title), do: String.trim(title), else: ""

      cond do
        not is_binary(title) -> [title: "must be a string"]
        trimmed == "" -> [title: "must not be blank"]
        String.length(trimmed) > 500 -> [title: "must be at most 500 characters"]
        true -> []
      end
    end)
    |> update_change(:title, fn
      title when is_binary(title) -> String.trim(title)
      title -> title
    end)
  end
end
