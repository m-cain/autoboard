defmodule Autoboard.ProjectsTest do
  use Autoboard.DataCase, async: true

  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects

  setup do
    %{ctx: Context.global(:me)}
  end

  test "project keys are normalized once and remain immutable", %{ctx: ctx} do
    assert {:ok, project} =
             Projects.create(ctx, %{key: "auto", name: "Autoboard", description: ""})

    assert project.key == "AUTO"
    assert project.revision == 1

    assert {:error, %Error{kind: :validation_failed}} =
             Projects.update(ctx, project.id, project.revision, %{key: "NEW"})
  end

  test "stale project writes return the current project", %{ctx: ctx} do
    project = project_fixture(ctx)
    assert {:ok, updated} = Projects.update(ctx, project.id, 1, %{name: "Renamed"})

    assert {:error, %Error{kind: :revision_conflict, current: ^updated}} =
             Projects.archive(ctx, project.id, 1)
  end

  defp project_fixture(ctx) do
    assert {:ok, project} =
             Projects.create(ctx, %{key: "auto", name: "Autoboard", description: ""})

    project
  end
end
