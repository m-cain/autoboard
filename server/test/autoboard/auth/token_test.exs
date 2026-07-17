defmodule Autoboard.Auth.TokenTest do
  use Autoboard.DataCase, async: true

  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Token

  test "tokens authenticate without storing plaintext" do
    assert {:ok, plaintext, token} = Token.issue(:codex)
    refute token.digest == plaintext
    assert {:ok, %Context{actor: :codex, scope: :global}} = Token.authenticate(plaintext)
  end
end
