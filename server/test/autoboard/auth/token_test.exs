defmodule Autoboard.Auth.TokenTest do
  use Autoboard.DataCase, async: true

  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Token
  alias Autoboard.Domain.Error
  alias Autoboard.Repo

  test "tokens authenticate without storing plaintext" do
    assert {:ok, plaintext, token} = Token.issue(:codex)
    refute token.digest == plaintext
    assert {:ok, %Context{actor: :codex, scope: :global}} = Token.authenticate(plaintext)
  end

  test "invalid and revoked tokens are unauthorized" do
    assert {:error, %Error{kind: :unauthorized}} = Token.authenticate("ab_invalid")

    assert {:ok, plaintext, token} = Token.issue(:me)

    assert {:ok, _revoked} =
             Repo.update(Token.changeset(token, %{revoked_at: DateTime.utc_now()}))

    assert {:error, %Error{kind: :unauthorized}} = Token.authenticate(plaintext)
  end
end
