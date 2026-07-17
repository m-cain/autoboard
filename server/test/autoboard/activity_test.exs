defmodule Autoboard.ActivityTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Activity
  alias Autoboard.Auth.Context
  alias Autoboard.Projects
  alias Autoboard.Repo

  test "broadcasts committed events and never broadcasts rolled back events" do
    :ok = Activity.subscribe()
    on_exit(fn -> Activity.unsubscribe() end)

    project = project_fixture(Context.global(:me), "AUTO")
    assert_receive {:activity, %{event_type: "project.created", project_id: project_id}}, 100
    assert project_id == project.id

    assert {:error, :rolled_back} =
             Activity.commit(fn ->
               Repo.rollback(:rolled_back)
             end)

    refute_receive {:activity, _event}, 50
  end

  test "replays events after an activity id" do
    ctx = Context.global(:me)
    first = project_fixture(ctx, "AUTO")
    second = project_fixture(ctx, "OTHR")

    assert {:ok, [first_event, second_event]} = Activity.replay_after(ctx, 0)
    assert first_event.project_id == first.id
    assert second_event.project_id == second.id
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} = Projects.create(ctx, %{key: key, name: "Project #{key}"})
    project
  end
end
