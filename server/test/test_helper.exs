ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(Autoboard.Repo, :manual)

ExUnit.after_suite(fn _results ->
  path = Application.fetch_env!(:autoboard, :socket_path)
  _ = Application.stop(:autoboard)

  wait_for_removal = fn
    _wait_for_removal, 0 ->
      not File.exists?(path)

    wait_for_removal, attempts ->
      if File.exists?(path),
        do:
          (
            Process.sleep(10)
            wait_for_removal.(wait_for_removal, attempts - 1)
          ),
        else: true
  end

  unless wait_for_removal.(wait_for_removal, 20) do
    {uid, 0} = System.cmd("id", ["-u"])
    current_uid = String.to_integer(String.trim(uid))

    case File.lstat(path) do
      {:ok, %{type: :other, mode: mode, uid: owner}}
      when Bitwise.band(mode, 0o170000) == 0o140000 and
             owner == current_uid ->
        case :gen_tcp.connect(
               {:local, String.to_charlist(path)},
               0,
               [:binary, active: false],
               100
             ) do
          {:ok, socket} ->
            :gen_tcp.close(socket)
            raise("live test socket must not be removed: #{path}")

          {:error, _reason} ->
            :ok = File.rm(path)
        end

      _ ->
        raise("test socket residue: #{path}")
    end
  end
end)
