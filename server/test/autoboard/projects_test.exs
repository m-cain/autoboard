defmodule Autoboard.ProjectsTest do
  use Autoboard.DataCase, async: true

  import Ecto.Query

  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
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

  test "unsupported project updates roll back state and activity", %{ctx: ctx} do
    project = project_fixture(ctx)

    assert {:error, %Error{kind: :validation_failed, fields: %{state: ["is not allowed"]}}} =
             Projects.update(ctx, project.id, project.revision, %{state: :archived})

    assert {:ok, unchanged} = Projects.fetch(ctx, project.id)
    assert unchanged.revision == project.revision
    assert unchanged.state == :active
    assert event_count(project.id) == 1
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
end
