defmodule Autoboard.ContractSchemaTest do
  use ExUnit.Case, async: true

  @fixtures Path.expand("../fixtures/contracts", __DIR__)
  @schemas Path.expand("../../../packages/contracts/generated", __DIR__)

  test "generated OpenAPI schemas validate every frozen Elixir transport fixture" do
    assert_valid("project-board.schema.json", "project_board.json")
    assert_valid("ticket-detail.schema.json", "ticket_detail.json")
    assert_valid("ticket-detail.schema.json", "parent_ticket_detail.json")
    assert_valid("attachment-rpc.schema.json", "attachment_rpc.json")
    assert_valid("rpc-failure.schema.json", "error_current.json")
  end

  defp assert_valid(schema_name, fixture_name) do
    schema = @schemas |> Path.join(schema_name) |> File.read!() |> Jason.decode!()
    fixture = @fixtures |> Path.join(fixture_name) |> File.read!() |> Jason.decode!()

    assert :ok = schema |> Xema.from_json_schema() |> Xema.validate(fixture)
  end
end
