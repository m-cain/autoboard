defmodule AutoboardWeb.RouterTest do
  use Autoboard.DataCase, async: false
  import Plug.Test

  alias Autoboard.Attachments
  alias Autoboard.Attachments.Attachment
  alias Autoboard.Activity.Event
  alias Autoboard.Auth.Context
  alias Autoboard.Auth.Token
  alias Autoboard.Comments.Comment
  alias Autoboard.Projects
  alias Autoboard.Repo
  alias Autoboard.Tickets
  alias Autoboard.Tickets.Dependency
  alias Autoboard.Tickets.Label
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

    assert {404, %{"error" => %{"kind" => "not_found"}}} =
             get_response("/api/v1/tickets/HTTP-#{String.duplicate("9", 1000)}")
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
    [disposition] = Plug.Conn.get_resp_header(response, "content-disposition")
    assert disposition =~ "attachment; filename=\""
    assert disposition =~ "filename*=UTF-8''"
    assert :ok = File.rm(attachment.managed_path)
  end

  test "has no write routes and never changes rows" do
    before = %{
      projects: Repo.aggregate(Autoboard.Projects.Project, :count),
      tickets: Repo.aggregate(Autoboard.Tickets.Ticket, :count),
      activity: Repo.aggregate(Event, :count),
      comments: Repo.aggregate(Comment, :count),
      attachments: Repo.aggregate(Attachment, :count),
      labels: Repo.aggregate(Label, :count),
      dependencies: Repo.aggregate(Dependency, :count),
      ticket_labels: table_count("ticket_labels"),
      access_tokens: Repo.aggregate(Token, :count)
    }

    for method <- [:post, :put, :patch, :delete],
        path <- [
          "/api/v1/projects",
          "/api/v1/triage",
          "/api/v1/projects/HTTP/board",
          "/api/v1/projects/HTTP/canceled",
          "/api/v1/tickets/HTTP-1",
          "/api/v1/attachments/00000000-0000-4000-8000-000000000000",
          "/api/v1/events",
          "/api/v1/unknown",
          "/api"
        ] do
      conn = conn(method, path) |> Router.call(@opts)
      assert conn.status == 404
    end

    assert before.projects == Repo.aggregate(Autoboard.Projects.Project, :count)
    assert before.tickets == Repo.aggregate(Autoboard.Tickets.Ticket, :count)
    assert before.activity == Repo.aggregate(Event, :count)
    assert before.comments == Repo.aggregate(Comment, :count)
    assert before.attachments == Repo.aggregate(Attachment, :count)
    assert before.labels == Repo.aggregate(Label, :count)
    assert before.dependencies == Repo.aggregate(Dependency, :count)
    assert before.ticket_labels == table_count("ticket_labels")
    assert before.access_tokens == Repo.aggregate(Token, :count)
  end

  test "malformed JSON write bodies are still inert 404s" do
    for method <- [:post, :put, :patch, :delete],
        path <- ["/api/v1/projects", "/api/v1/events", "/api"] do
      response =
        conn(method, path, "{not json")
        |> Plug.Conn.put_req_header("content-type", "application/json")
        |> Router.call(@opts)

      assert response.status == 404
    end
  end

  test "reports health through an injectable database check" do
    previous = Application.get_env(:autoboard, :health_check)
    on_exit(fn -> Application.put_env(:autoboard, :health_check, previous) end)

    Application.put_env(:autoboard, :health_check, fn -> :ok end)
    assert {200, %{"status" => "ok"}} = get_response("/health")

    Application.put_env(:autoboard, :health_check, fn -> {:error, :down} end)
    assert {503, %{"status" => "unavailable"}} = get_response("/health")
  end

  test "never treats browser paths as mutation fallbacks" do
    for method <- [:post, :put, :patch, :delete] do
      response = conn(method, "/projects") |> Router.call(@opts)
      assert response.status == 404
    end

    assert (conn(:get, "/assets/missing.js") |> Router.call(@opts)).status == 404
    assert (conn(:get, "/../config/config.exs") |> Router.call(@opts)).status == 404
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

  defp table_count(table) do
    %{rows: [[count]]} = Ecto.Adapters.SQL.query!(Repo, "SELECT count(*) FROM #{table}")
    count
  end
end
