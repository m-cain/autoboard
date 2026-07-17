defmodule Autoboard.ProjectsTest do
  use Autoboard.DataCase, async: true

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Projects.Project
  alias Autoboard.Repo

  setup do
    %{ctx: Context.global(:me)}
  end

  test "project keys are normalized once and remain immutable", %{ctx: ctx} do
    assert {:ok, project} =
             Projects.create(ctx, %{key: "auto", name: "Autoboard", description: ""})

    assert project.key == "AUTO"
    assert project.revision == 1

    assert {:error, %Error{kind: :validation_failed, fields: %{key: ["is immutable"]}}} =
             Projects.update(ctx, project.id, project.revision, %{key: "NEW"})
  end

  test "stale project writes return the current project", %{ctx: ctx} do
    project = project_fixture(ctx)
    assert {:ok, updated} = Projects.update(ctx, project.id, 1, %{name: "Renamed"})

    assert {:error, %Error{kind: :revision_conflict, current: ^updated}} =
             Projects.archive(ctx, project.id, 1)
  end

  test "fetch rejects malformed project IDs", %{ctx: ctx} do
    assert_invalid_project_id(Projects.fetch(ctx, "not-a-uuid"))
  end

  test "update rejects malformed project IDs", %{ctx: ctx} do
    assert_invalid_project_id(Projects.update(ctx, "not-a-uuid", 1, %{name: "Renamed"}))
  end

  test "archive rejects malformed project IDs", %{ctx: ctx} do
    assert_invalid_project_id(Projects.archive(ctx, "not-a-uuid", 1))
  end

  test "restore rejects malformed project IDs", %{ctx: ctx} do
    assert_invalid_project_id(Projects.restore(ctx, "not-a-uuid", 1))
  end

  test "project creation rejects protected and unknown attributes atomically", %{ctx: ctx} do
    for extra_attrs <- [
          %{state: :archived},
          %{revision: 99},
          %{next_ticket_number: 99},
          %{unexpected: "value"}
        ] do
      assert {:error, %Error{kind: :validation_failed}} =
               Projects.create(
                 ctx,
                 Map.merge(%{key: "AUTO", name: "Autoboard", description: ""}, extra_attrs)
               )
    end

    assert Repo.aggregate(Project, :count) == 0
    assert Repo.aggregate(Event, :count) == 0
  end

  test "project creation rejects nil and non-string text attributes atomically", %{ctx: ctx} do
    for {field, value} <- invalid_text_attributes() do
      attrs = Map.put(%{key: "AUTO", name: "Autoboard", description: ""}, field, value)
      assert_invalid_text_field(Projects.create(ctx, attrs), normalize_field(field))
    end

    for {field, value} <- invalid_text_attributes() do
      attrs =
        Map.put(
          %{"key" => "AUTO", "name" => "Autoboard", "description" => ""},
          Atom.to_string(field),
          value
        )

      assert_invalid_text_field(Projects.create(ctx, attrs), normalize_field(field))
    end

    assert Repo.aggregate(Project, :count) == 0
    assert Repo.aggregate(Event, :count) == 0
  end

  test "project updates reject nil and non-string text attributes without mutation", %{ctx: ctx} do
    project = project_fixture(ctx)

    for {field, value} <- invalid_text_attributes() do
      assert_invalid_text_field(
        Projects.update(ctx, project.id, project.revision, %{field => value}),
        normalize_field(field)
      )
    end

    for {field, value} <- invalid_text_attributes() do
      assert_invalid_text_field(
        Projects.update(ctx, project.id, project.revision, %{Atom.to_string(field) => value}),
        normalize_field(field)
      )
    end

    assert_unchanged_project(ctx, project)
  end

  test "create and update validate non-map attrs", %{ctx: ctx} do
    project = project_fixture(ctx)

    for attrs <- [nil, []] do
      assert_invalid_attrs(Projects.create(ctx, attrs))
      assert_invalid_attrs(Projects.update(ctx, project.id, project.revision, attrs))
    end

    assert_unchanged_project(ctx, project)
  end

  test "project mutations require positive integer revisions", %{ctx: ctx} do
    project = project_fixture(ctx)

    operations = [
      fn revision -> Projects.update(ctx, project.id, revision, %{name: "Renamed"}) end,
      fn revision -> Projects.archive(ctx, project.id, revision) end,
      fn revision -> Projects.restore(ctx, project.id, revision) end
    ]

    for operation <- operations, revision <- ["1", 0, -1] do
      assert_invalid_revision(operation.(revision))
    end

    assert_unchanged_project(ctx, project)
  end

  test "invalid contexts take precedence over public command argument validation" do
    invalid_ctx = %Context{actor: :system, scope: :global}

    operations = [
      fn -> Projects.create(invalid_ctx, []) end,
      fn -> Projects.update(invalid_ctx, "not-a-uuid", "1", []) end,
      fn -> Projects.archive(invalid_ctx, "not-a-uuid", "1") end,
      fn -> Projects.restore(invalid_ctx, "not-a-uuid", "1") end
    ]

    for operation <- operations do
      assert {:error, %Error{kind: :unauthorized}} = operation.()
    end

    assert Repo.aggregate(Project, :count) == 0
    assert Repo.aggregate(Event, :count) == 0
  end

  test "unsupported project updates roll back state and activity", %{ctx: ctx} do
    project = project_fixture(ctx)

    assert {:error, %Error{kind: :validation_failed, fields: %{state: ["is not allowed"]}}} =
             Projects.update(ctx, project.id, project.revision, %{state: :archived})

    assert {:ok, unchanged} = Projects.fetch(ctx, project.id)
    assert unchanged.revision == project.revision
    assert unchanged.state == :active
    assert event_count(project.id) == 1
  end

  test "empty project updates leave revision and activity unchanged", %{ctx: ctx} do
    project = project_fixture(ctx)

    assert {:error, %Error{kind: :validation_failed}} =
             Projects.update(ctx, project.id, project.revision, %{})

    assert_unchanged_project(ctx, project)
  end

  test "same-value project updates leave revision and activity unchanged", %{ctx: ctx} do
    project = project_fixture(ctx)

    assert {:error, %Error{kind: :validation_failed}} =
             Projects.update(ctx, project.id, project.revision, %{
               name: project.name,
               description: project.description
             })

    assert_unchanged_project(ctx, project)
  end

  test "update activity records changed user fields with old and new values", %{ctx: ctx} do
    project = project_fixture(ctx)

    assert {:ok, _updated} =
             Projects.update(ctx, project.id, project.revision, %{
               name: "Renamed",
               description: "New description"
             })

    assert %Event{event_type: "project.updated", payload: payload} = latest_event(project.id)

    assert payload == %{
             "name" => %{"from" => "Autoboard", "to" => "Renamed"},
             "description" => %{"from" => "", "to" => "New description"}
           }
  end

  test "archived projects reject mutation and restoration records state changes", %{ctx: ctx} do
    project = project_fixture(ctx)
    assert {:ok, archived} = Projects.archive(ctx, project.id, project.revision)
    assert archived.state == :archived

    assert %Event{event_type: "project.archived", payload: %{"state" => state_change}} =
             latest_event(project.id)

    assert state_change == %{"from" => "active", "to" => "archived"}

    assert {:error, %Error{kind: :validation_failed}} =
             Projects.update(ctx, project.id, archived.revision, %{name: "Blocked"})

    assert event_count(project.id) == 2

    assert {:ok, restored} = Projects.restore(ctx, project.id, archived.revision)
    assert restored.state == :active

    assert %Event{event_type: "project.restored", payload: %{"state" => restored_change}} =
             latest_event(project.id)

    assert restored_change == %{"from" => "archived", "to" => "active"}
  end

  test "list places active projects first and sorts names case insensitively", %{ctx: ctx} do
    active_zeta = project_fixture(ctx, %{key: "ZETA", name: "zeta"})
    _active_alpha = project_fixture(ctx, %{key: "ALPHA", name: "Alpha"})
    archived_aardvark = project_fixture(ctx, %{key: "AARD", name: "aardvark"})
    assert {:ok, _archived} = Projects.archive(ctx, archived_aardvark.id, 1)

    assert {:ok, projects} = Projects.list(ctx)
    assert Enum.map(projects, & &1.name) == ["Alpha", active_zeta.name, archived_aardvark.name]
  end

  test "project validation rejects malformed keys, blank names, and non-string descriptions", %{
    ctx: ctx
  } do
    for attrs <- [
          %{key: "1BAD", name: "Autoboard", description: ""},
          %{key: "AUTO", name: "   ", description: ""},
          %{key: "AUTO", name: "Autoboard", description: 42}
        ] do
      assert {:error, %Error{kind: :validation_failed}} = Projects.create(ctx, attrs)
    end
  end

  test "project key validation describes the two-character minimum", %{ctx: ctx} do
    assert {:error,
            %Error{
              kind: :validation_failed,
              fields: %{
                key: ["must be 2-8 uppercase alphanumeric characters and begin with a letter"]
              }
            }} = Projects.create(ctx, %{key: "A", name: "Autoboard", description: ""})
  end

  defp project_fixture(ctx, overrides \\ %{}) do
    assert {:ok, project} =
             Projects.create(
               ctx,
               Map.merge(%{key: "auto", name: "Autoboard", description: ""}, overrides)
             )

    project
  end

  defp latest_event(project_id) do
    Repo.one!(
      from(event in Event,
        where: event.project_id == ^project_id,
        order_by: [desc: event.id],
        limit: 1
      )
    )
  end

  defp event_count(project_id) do
    Repo.aggregate(from(event in Event, where: event.project_id == ^project_id), :count)
  end

  defp assert_unchanged_project(ctx, project) do
    assert {:ok, unchanged} = Projects.fetch(ctx, project.id)
    assert unchanged.revision == project.revision
    assert unchanged.name == project.name
    assert unchanged.description == project.description
    assert event_count(project.id) == 1
  end

  defp assert_invalid_project_id(result) do
    assert {:error, %Error{kind: :validation_failed, fields: %{id: [_message]}}} = result
  end

  defp assert_invalid_attrs(result) do
    assert {:error, %Error{kind: :validation_failed, fields: %{attrs: [_message]}}} = result
  end

  defp assert_invalid_revision(result) do
    assert {:error, %Error{kind: :validation_failed, fields: %{expected_revision: [_message]}}} =
             result
  end

  defp invalid_text_attributes do
    [name: nil, name: 42, description: nil, description: 42]
  end

  defp normalize_field(field) when is_atom(field), do: field
  defp normalize_field(field), do: String.to_existing_atom(field)

  defp assert_invalid_text_field(result, field) do
    assert {:error, %Error{kind: :validation_failed, fields: fields}} = result
    assert Map.has_key?(fields, field)
  end
end
