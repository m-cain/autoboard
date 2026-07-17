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

  test "generated timestamp schemas reject impossible calendar dates and malformed clock values" do
    schema = @schemas |> Path.join("project.schema.json") |> File.read!() |> Jason.decode!()

    project =
      @fixtures
      |> Path.join("project_board.json")
      |> File.read!()
      |> Jason.decode!()
      |> Map.fetch!("project")

    xema = Xema.from_json_schema(schema)

    assert {:error, _} = Xema.validate(xema, %{project | "inserted_at" => "2026-02-30T00:00:00Z"})
    assert {:error, _} = Xema.validate(xema, %{project | "inserted_at" => "2026-07-16T25:00:00Z"})

    assert {:error, _} =
             Xema.validate(xema, %{project | "inserted_at" => "2026-07-16T12:34:56+00:00"})

    assert :ok = Xema.validate(xema, %{project | "inserted_at" => "2026-07-16T12:34:56.123456Z"})

    assert {:error, _} =
             Xema.validate(xema, %{project | "inserted_at" => "2026-07-16T12:34:56.1234567Z"})
  end

  test "generated RPC envelope schema accepts the Task 7 domain, protocol, and internal shapes" do
    schema =
      @schemas |> Path.join("rpc-envelope-failure.schema.json") |> File.read!() |> Jason.decode!()

    xema = Xema.from_json_schema(schema)

    for envelope <- [
          %{
            "jsonrpc" => "2.0",
            "id" => 1,
            "error" => %{
              "code" => -32010,
              "message" => "no access",
              "data" => %{"kind" => "unauthorized", "message" => "no access", "fields" => %{}}
            }
          },
          %{
            "jsonrpc" => "2.0",
            "id" => nil,
            "error" => %{
              "code" => -32600,
              "message" => "Invalid Request",
              "data" => %{"kind" => "invalid_request"}
            }
          },
          %{
            "jsonrpc" => "2.0",
            "id" => 3,
            "error" => %{
              "code" => -32010,
              "message" => "Internal error",
              "data" => %{"kind" => "internal_error", "correlation_id" => "abc"}
            }
          }
        ] do
      assert :ok = Xema.validate(xema, envelope)
    end
  end

  defp assert_valid(schema_name, fixture_name) do
    schema = @schemas |> Path.join(schema_name) |> File.read!() |> Jason.decode!()
    fixture = @fixtures |> Path.join(fixture_name) |> File.read!() |> Jason.decode!()

    assert :ok = schema |> Xema.from_json_schema() |> Xema.validate(fixture)
  end
end
