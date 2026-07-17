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
end
