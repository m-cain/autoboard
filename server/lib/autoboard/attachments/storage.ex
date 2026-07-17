defmodule Autoboard.Attachments.Storage do
  @chunk_size 65_536

  @spec stage(String.t()) :: {:ok, map()} | {:error, atom()}
  def stage(source_path) when is_binary(source_path) do
    with :ok <- require_absolute(source_path),
         {:ok, stat} <- regular_file_stat(source_path),
         :ok <- enforce_size(stat.size),
         :ok <- ensure_managed_dirs() do
      staged_path = Path.join(tmp_dir(), Ecto.UUID.generate())

      case stage_copy(source_path, staged_path, stat) do
        {:ok, %{byte_size: byte_size, sha256: sha256}} ->
          {:ok,
           %{
             staged_path: staged_path,
             original_filename: Path.basename(source_path),
             media_type: media_type(source_path),
             byte_size: byte_size,
             sha256: sha256
           }}

        {:error, reason} ->
          File.rm(staged_path)
          {:error, reason}
      end
    end
  end

  def stage(_source_path), do: {:error, :not_absolute}

  @spec tmp_dir() :: String.t()
  def tmp_dir, do: Path.join([data_dir(), "attachments", "tmp"])

  @spec final_dir() :: String.t()
  def final_dir, do: Path.join([data_dir(), "attachments"])

  @spec final_path(Ecto.UUID.t()) :: String.t()
  def final_path(id), do: Path.join(final_dir(), id)

  @spec ensure_managed_dirs() :: :ok | {:error, atom()}
  def ensure_managed_dirs do
    with :ok <- File.mkdir_p(final_dir()),
         :ok <- File.chmod(final_dir(), 0o700),
         :ok <- File.mkdir_p(tmp_dir()),
         :ok <- File.chmod(tmp_dir(), 0o700) do
      :ok
    end
  end

  defp data_dir, do: Application.fetch_env!(:autoboard, :data_dir)
  defp max_bytes, do: Application.fetch_env!(:autoboard, :max_attachment_bytes)

  defp require_absolute(path) do
    if Path.type(path) == :absolute, do: :ok, else: {:error, :not_absolute}
  end

  defp regular_file_stat(path) do
    case File.lstat(path, time: :posix) do
      {:ok, %{type: :regular} = stat} -> {:ok, stat}
      {:ok, _stat} -> {:error, :not_regular}
      {:error, _reason} -> {:error, :unreadable}
    end
  end

  defp enforce_size(size) do
    if size <= max_bytes(), do: :ok, else: {:error, :too_large}
  end

  defp stage_copy(source_path, staged_path, initial_stat) do
    try do
      copy_and_hash(source_path, staged_path, initial_stat)
    rescue
      _error -> {:error, :unreadable}
    catch
      _kind, _reason -> {:error, :unreadable}
    end
  end

  defp copy_and_hash(source_path, staged_path, initial_stat) do
    case File.open(source_path, [:read, :binary, :raw]) do
      {:ok, source} ->
        try do
          case File.open(staged_path, [:write, :binary, :raw]) do
            {:ok, target} ->
              try do
                with :ok <- File.chmod(staged_path, 0o600),
                     :ok <- confirm_open_regular_file(source, initial_stat),
                     {:ok, hash, byte_size} <-
                       copy_chunks(source, target, :crypto.hash_init(:sha256), 0) do
                  {:ok,
                   %{
                     byte_size: byte_size,
                     sha256: Base.encode16(:crypto.hash_final(hash), case: :lower)
                   }}
                end
              after
                File.close(target)
              end

            {:error, _reason} ->
              {:error, :unreadable}
          end
        after
          File.close(source)
        end

      {:error, _reason} ->
        {:error, :unreadable}
    end
  end

  defp copy_chunks(source, target, hash, byte_size) do
    case IO.binread(source, @chunk_size) do
      :eof ->
        {:ok, hash, byte_size}

      {:error, _reason} ->
        {:error, :unreadable}

      chunk when is_binary(chunk) ->
        next_byte_size = byte_size + byte_size(chunk)

        if next_byte_size > max_bytes() do
          {:error, :too_large}
        else
          case IO.binwrite(target, chunk) do
            :ok ->
              maybe_test_hook(:after_chunk, next_byte_size)

              copy_chunks(
                source,
                target,
                :crypto.hash_update(hash, chunk),
                next_byte_size
              )

            {:error, _reason} ->
              {:error, :unreadable}
          end
        end
    end
  end

  defp confirm_open_regular_file(source, initial_stat) do
    with {:ok,
          {:file_info, size, :regular, _access, _atime, _mtime, _ctime, _mode, _links,
           major_device, minor_device, inode, _uid, _gid}} <- :file.read_file_info(source),
         true <-
           size == initial_stat.size and inode == initial_stat.inode and
             major_device == initial_stat.major_device and
             minor_device == initial_stat.minor_device do
      :ok
    else
      _ -> {:error, :source_changed}
    end
  end

  defp maybe_test_hook(stage, byte_size) do
    case Application.get_env(:autoboard, :attachment_storage_hook) do
      fun when is_function(fun, 2) -> fun.(stage, byte_size)
      _ -> :ok
    end
  end

  defp media_type(path) do
    case path |> Path.extname() |> String.downcase() do
      ".txt" -> "text/plain"
      ".md" -> "text/markdown"
      ".json" -> "application/json"
      ".csv" -> "text/csv"
      ".html" -> "text/html"
      ".xml" -> "application/xml"
      ".pdf" -> "application/pdf"
      ".png" -> "image/png"
      ".jpg" -> "image/jpeg"
      ".jpeg" -> "image/jpeg"
      ".gif" -> "image/gif"
      _ -> "application/octet-stream"
    end
  end
end
