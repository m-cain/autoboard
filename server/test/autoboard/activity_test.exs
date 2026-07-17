defmodule Autoboard.ActivityTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Activity
  alias Autoboard.Auth.Context
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias Autoboard.Tickets

  setup do
    previous_broadcast = Application.get_env(:autoboard, :activity_broadcast)

    on_exit(fn -> Application.put_env(:autoboard, :activity_broadcast, previous_broadcast) end)
  end

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

  test "rejects nested ownership before broadcasting an outer rolled-back event" do
    :ok = Activity.subscribe()
    on_exit(fn -> Activity.unsubscribe() end)
    ctx = Context.global(:me)
    project = project_fixture(ctx, "AUTO")

    assert {:error, :outer_rollback} =
             Repo.transaction(fn ->
               assert {:ok, event} =
                        Activity.append(ctx, "project.updated", project.id, nil, %{
                          "name" => "Later"
                        })

               assert {:error, :nested_transaction} =
                        Activity.commit(fn -> {event, [event]} end)

               Repo.rollback(:outer_rollback)
             end)

    refute_receive {:activity, %{event_type: "project.updated"}}, 50
    assert {:ok, events} = Activity.replay_after(ctx, 0)
    refute Enum.any?(events, &(&1.event_type == "project.updated"))
  end

  test "retains a committed mutation when broadcasting raises" do
    Application.put_env(:autoboard, :activity_broadcast, fn _event ->
      raise "subscriber unavailable"
    end)

    assert {:ok, project} =
             Projects.create(Context.global(:me), %{key: "AUTO", name: "Project AUTO"})

    assert project.key == "AUTO"
    assert {:ok, [_event]} = Activity.replay_after(Context.global(:me), 0)
  end

  test "broadcasts every committed transition event in order" do
    ctx = Context.global(:me)
    project = project_fixture(ctx, "AUTO")
    blocker = ticket_fixture(ctx, project, "Blocker")
    blocked = ticket_fixture(ctx, project, "Blocked")

    assert {:ok, blocked} = Tickets.add_dependency(ctx, blocked.id, blocker.id, blocked.revision)
    :ok = Activity.subscribe()
    on_exit(fn -> Activity.unsubscribe() end)

    assert {:ok, _blocker} = Tickets.transition(ctx, blocker.id, blocker.revision, :done)

    assert_receive {:activity, %{event_type: "ticket.transitioned", ticket_id: blocker_id}}, 100
    assert blocker_id == blocker.id

    assert_receive {:activity,
                    %{event_type: "dependency.blocking_changed", ticket_id: blocked_id}},
                   100

    assert blocked_id == blocked.id
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} = Projects.create(ctx, %{key: key, name: "Project #{key}"})
    project
  end

  defp ticket_fixture(ctx, project, title) do
    assert {:ok, ticket} = Tickets.create(ctx, %{project_id: project.id, title: title})
    ticket
  end
end
