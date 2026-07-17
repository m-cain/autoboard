defmodule AutoboardWeb.RouterTest do
  use Autoboard.DataCase, async: false
  import Plug.Test

  alias Autoboard.Auth.Context
  alias Autoboard.Attachments
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias Autoboard.Tickets
  alias AutoboardWeb.Router

  @opts Router.init([])

  setup do
    ctx = Context.global(:codex)
    {:ok, project} = Projects.create(ctx, %{key: "HTTP", name: "HTTP project", description: ""})
    {:ok, triage} = Tickets.create(ctx, %{project_id: project.id, title: "Triage"})

    {:ok, board} =
      Tickets.create(ctx, %{
        project_id: project.id,
        title: "Board",
        status: :ready,
        assignee: :codex
      })

    %{project: project, triage: triage, board: board}
  end

  test "serves every read projection as JSON", %{triage: triage, board: board} do
    assert %{"active" => [%{"key" => "HTTP"}], "archived" => []} = get("/api/v1/projects")
    triage_identifier = triage.identifier
    assert %{"tickets" => [%{"identifier" => ^triage_identifier}]} = get("/api/v1/triage")

    assert %{"project" => %{"key" => "HTTP"}, "columns" => columns} =
             get("/api/v1/projects/http/board")

    board_identifier = board.identifier
    assert [%{"identifier" => ^board_identifier}] = columns["ready"]
    assert %{"tickets" => []} = get("/api/v1/projects/HTTP/canceled")
    assert %{"identifier" => ^board_identifier} = get("/api/v1/tickets/#{board_identifier}")
  end

  test "returns safe validation and not found envelopes" do
    assert {400, %{"error" => %{"kind" => "validation_failed"}}} =
             get_response("/api/v1/projects/invalid!/board")

    assert {404, %{"error" => %{"kind" => "not_found"}}} =
             get_response("/api/v1/tickets/HTTP-999")

    assert {400, %{"error" => %{"kind" => "validation_failed"}}} =
             get_response("/api/v1/attachments/not-a-uuid")

    assert {400, %{"error" => %{"kind" => "validation_failed"}}} =
             get_response("/api/v1/events", "last-event-id", "1.5")
  end

  test "downloads an attachment without exposing its managed path", %{board: board} do
    source = Path.join(System.tmp_dir!(), "autoboard-http-#{Ecto.UUID.generate()}.txt")
    File.write!(source, "read-only attachment")
    on_exit(fn -> File.rm(source) end)

    assert {:ok, attachment} = Attachments.add_from_path(Context.global(:codex), board.id, source)

    response = conn(:get, "/api/v1/attachments/#{attachment.id}") |> Router.call(@opts)
    assert response.status == 200
    assert response.resp_body == "read-only attachment"
    assert ["text/plain; charset=utf-8"] = Plug.Conn.get_resp_header(response, "content-type")
    assert ["attachment"] = Plug.Conn.get_resp_header(response, "content-disposition")
    assert :ok = File.rm(attachment.managed_path)
  end

  test "has no write routes and never changes rows" do
    before = %{
      projects: Repo.aggregate(Autoboard.Projects.Project, :count),
      tickets: Repo.aggregate(Autoboard.Tickets.Ticket, :count)
    }

    for method <- [:post, :put, :patch, :delete],
        path <- [
          "/api/v1/projects",
          "/api/v1/projects/HTTP/board",
          "/api/v1/tickets/HTTP-1",
          "/api/v1/events"
        ] do
      conn = conn(method, path) |> Router.call(@opts)
      assert conn.status == 404
    end

    assert before.projects == Repo.aggregate(Autoboard.Projects.Project, :count)
    assert before.tickets == Repo.aggregate(Autoboard.Tickets.Ticket, :count)
  end

  test "reports health through an injectable database check" do
    previous = Application.get_env(:autoboard, :health_check)
    on_exit(fn -> Application.put_env(:autoboard, :health_check, previous) end)

    Application.put_env(:autoboard, :health_check, fn -> :ok end)
    assert {200, %{"status" => "ok"}} = get_response("/health")

    Application.put_env(:autoboard, :health_check, fn -> {:error, :down} end)
    assert {503, %{"status" => "unavailable"}} = get_response("/health")
  end

  defp get(path), do: get_response(path) |> elem(1)

  defp get_response(path, header_name \\ nil, header_value \\ nil) do
    request = conn(:get, path)

    request =
      if header_name,
        do: Plug.Conn.put_req_header(request, header_name, header_value),
        else: request

    response = Router.call(request, @opts)
    {response.status, Jason.decode!(response.resp_body)}
  end
end
