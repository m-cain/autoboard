defmodule Autoboard.RPC.RouterTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.RPC.Router

  test "rejects missing and malformed RPC method parameters as domain validation errors" do
    ctx = Context.global(:codex)

    assert {:error, %Error{kind: :validation_failed, fields: %{key: _}}} =
             Router.dispatch(ctx, "projects.create", %{"name" => "Missing key"})

    assert {:error, %Error{kind: :validation_failed, fields: %{expected_revision: _}}} =
             Router.dispatch(ctx, "projects.archive", %{"project_id" => "not-a-uuid"})
  end

  test "rejects unknown methods without JSON-RPC envelope knowledge" do
    assert {:error, %Error{kind: :method_not_found, message: "method not found"}} =
             Router.dispatch(Context.global(:codex), "missing.method", %{})
  end

  test "routes every public method through real contexts with stable result shapes" do
    ctx = Context.global(:codex)

    source =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-router-#{System.unique_integer([:positive])}.txt"
      )

    :ok = File.write(source, "router attachment")
    on_exit(fn -> File.rm(source) end)

    assert {:ok, project} =
             Router.dispatch(ctx, "projects.create", %{"key" => "AUTO", "name" => "Autoboard"})

    assert {:ok, %{"active" => [_], "archived" => []}} =
             Router.dispatch(ctx, "projects.list", %{})

    assert {:ok, %{"id" => project_id}} =
             Router.dispatch(ctx, "projects.get", %{"project_id" => "AUTO"})

    assert project_id == project["id"]

    assert {:ok, parent} =
             Router.dispatch(ctx, "tickets.create", %{
               "project_id" => project_id,
               "title" => "Parent"
             })

    assert {:ok, blocker} =
             Router.dispatch(ctx, "tickets.create", %{
               "project_id" => project_id,
               "title" => "Blocker"
             })

    identifier = parent["identifier"]

    assert {:ok, %{"identifier" => ^identifier}} =
             Router.dispatch(ctx, "tickets.get", %{"ticket_id" => identifier})

    assert {:ok, %{"columns" => _}} =
             Router.dispatch(ctx, "tickets.board", %{"project_id" => "AUTO"})

    assert {:ok, %{"tickets" => [_ | _]}} =
             Router.dispatch(ctx, "tickets.search", %{"query" => "Parent"})

    assert {:ok, %{"tickets" => []}} = Router.dispatch(ctx, "tickets.actionable", %{})

    assert {:ok, updated} =
             Router.dispatch(ctx, "tickets.update", %{
               "ticket_id" => identifier,
               "expected_revision" => parent["revision"],
               "priority" => "high"
             })

    assert {:ok, transitioned} =
             Router.dispatch(ctx, "tickets.transition", %{
               "ticket_id" => parent["id"],
               "expected_revision" => updated["revision"],
               "status" => "ready"
             })

    assert {:ok, %{"ticket_revision" => comment_revision}} =
             Router.dispatch(ctx, "comments.add", %{"ticket_id" => identifier, "body" => "note"})

    assert comment_revision == transitioned["revision"] + 1

    assert {:ok, attachment} =
             Router.dispatch(ctx, "attachments.add_from_path", %{
               "ticket_id" => identifier,
               "path" => source
             })

    assert attachment["ticket_revision"] == comment_revision + 1

    assert {:ok, read_attachment} =
             Router.dispatch(ctx, "attachments.read", %{"attachment_id" => attachment["id"]})

    assert read_attachment["id"] == attachment["id"] and
             read_attachment["content"] == "router attachment"

    assert {:ok, dependency} =
             Router.dispatch(ctx, "dependencies.add", %{
               "blocked_ticket_id" => identifier,
               "blocker_ticket_id" => blocker["identifier"],
               "expected_revision" => attachment["ticket_revision"]
             })

    assert {:ok, removed} =
             Router.dispatch(ctx, "dependencies.remove", %{
               "blocked_ticket_id" => parent["id"],
               "blocker_ticket_id" => blocker["id"],
               "expected_revision" => dependency["revision"]
             })

    assert {:ok, archived} =
             Router.dispatch(ctx, "projects.archive", %{
               "project_id" => project_id,
               "expected_revision" => project["revision"]
             })

    assert {:ok, %{"state" => "active"}} =
             Router.dispatch(ctx, "projects.restore", %{
               "project_id" => project_id,
               "expected_revision" => archived["revision"]
             })

    assert removed["revision"] > dependency["revision"]
  end

  test "rejects injected params and maps stale revisions to structured errors" do
    ctx = Context.global(:codex)

    assert {:error, %Error{kind: :validation_failed}} =
             Router.dispatch(ctx, "projects.list", %{"actor" => "me", "evil" => true})

    assert {:ok, project} =
             Router.dispatch(ctx, "projects.create", %{"key" => "STALE", "name" => "Stale"})

    assert {:ok, _} =
             Router.dispatch(ctx, "projects.update", %{
               "project_id" => project["id"],
               "expected_revision" => 1,
               "name" => "New"
             })

    assert {:error, %Error{kind: :revision_conflict, current: _}} =
             Router.dispatch(ctx, "projects.archive", %{
               "project_id" => project["id"],
               "expected_revision" => 1
             })
  end
end
