defmodule Autoboard.AttachmentsTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Attachments
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Projects
  alias Autoboard.Tickets

  setup do
    data_dir = Path.join(System.tmp_dir!(), "autoboard-attachments-#{Ecto.UUID.generate()}")
    File.mkdir_p!(data_dir)

    previous_data_dir = Application.get_env(:autoboard, :data_dir)
    previous_max = Application.get_env(:autoboard, :max_attachment_bytes)
    Application.put_env(:autoboard, :data_dir, data_dir)

    on_exit(fn ->
      Application.put_env(:autoboard, :data_dir, previous_data_dir)
      Application.put_env(:autoboard, :max_attachment_bytes, previous_max)
      File.rm_rf(data_dir)
    end)

    %{ctx: Context.global(:me), data_dir: data_dir}
  end

  test "copies a local file with checksum metadata and reads small text inline", %{
    ctx: ctx,
    data_dir: data_dir
  } do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    source = write_source(data_dir, "note.txt", "hello")

    assert {:ok, attachment} = Attachments.add_from_path(ctx, ticket.id, source)
    assert attachment.original_filename == "note.txt"
    assert attachment.media_type == "text/plain"
    assert attachment.byte_size == 5
    assert attachment.sha256 == "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
    assert Path.type(attachment.managed_path) == :absolute
    assert File.regular?(attachment.managed_path)

    assert {:ok, fetched} = Attachments.fetch(ctx, attachment.id)
    assert fetched.id == attachment.id

    assert {:ok, %{attachment: ^attachment, content: "hello"}} =
             Attachments.read(ctx, attachment.id)
  end

  test "rejects relative, directory, and symlink source paths", %{ctx: ctx, data_dir: data_dir} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    source = write_source(data_dir, "source.txt", "hello")
    link = Path.join(data_dir, "source-link.txt")
    File.ln_s!(source, link)

    for path <- ["source.txt", data_dir, link] do
      assert {:error, %Error{kind: :validation_failed, fields: %{source_path: [_]}}} =
               Attachments.add_from_path(ctx, ticket.id, path)
    end
  end

  test "returns metadata and managed path for invalid UTF-8 text", %{ctx: ctx, data_dir: data_dir} do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    source = write_source(data_dir, "invalid.txt", <<255, 254>>)

    assert {:ok, attachment} = Attachments.add_from_path(ctx, ticket.id, source)

    assert {:ok, %{attachment: ^attachment, managed_path: managed_path}} =
             Attachments.read(ctx, attachment.id)

    assert managed_path == attachment.managed_path
  end

  test "enforces configured size cap before staging", %{ctx: ctx, data_dir: data_dir} do
    Application.put_env(:autoboard, :max_attachment_bytes, 3)
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    source = write_source(data_dir, "large.txt", "four")

    assert {:error, %Error{kind: :validation_failed, fields: %{source_path: [_]}}} =
             Attachments.add_from_path(ctx, ticket.id, source)

    refute File.exists?(Path.join([data_dir, "attachments", "tmp"]))
  end

  test "removes only the managed final file when the database transaction rolls back", %{
    ctx: ctx,
    data_dir: data_dir
  } do
    project = project_fixture(ctx, "AUTO")
    ticket = ticket_fixture(ctx, project)
    source = write_source(data_dir, "rollback.txt", "contents")
    assert {:ok, _archived} = Projects.archive(ctx, project.id, project.revision)

    assert {:error, %Error{kind: :validation_failed}} =
             Attachments.add_from_path(ctx, ticket.id, source)

    assert [] ==
             Path.wildcard(Path.join([data_dir, "attachments", "*"])) --
               [Path.join([data_dir, "attachments", "tmp"])]
  end

  test "cleanup removes stale temporary files but logs and keeps orphan final files", %{
    data_dir: data_dir
  } do
    tmp_dir = Path.join([data_dir, "attachments", "tmp"])
    final_dir = Path.join([data_dir, "attachments"])
    File.mkdir_p!(tmp_dir)
    stale = Path.join(tmp_dir, "stale")
    orphan = Path.join(final_dir, Ecto.UUID.generate())
    File.write!(stale, "stale")
    File.write!(orphan, "orphan")
    File.touch!(stale, {{2020, 1, 1}, {0, 0, 0}})

    assert :ok = Attachments.cleanup()
    refute File.exists?(stale)
    assert File.exists?(orphan)
  end

  defp project_fixture(ctx, key) do
    assert {:ok, project} = Projects.create(ctx, %{key: key, name: "Project #{key}"})
    project
  end

  defp ticket_fixture(ctx, project) do
    assert {:ok, ticket} = Tickets.create(ctx, %{project_id: project.id, title: "Ticket"})
    ticket
  end

  defp write_source(data_dir, filename, content) do
    path = Path.join(data_dir, filename)
    File.write!(path, content)
    path
  end
end
