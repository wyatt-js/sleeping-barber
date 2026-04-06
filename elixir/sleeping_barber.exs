defmodule ShopConfig do
  def customers, do: 20
  def capacity, do: 5
  def arrival_min, do: 500
  def arrival_max, do: 2000
  def duration_min, do: 1000
  def duration_max, do: 4000
  def satisfaction, do: 3.0
  def grace_period, do: 10000
end

defmodule ShopOwner do
  def start do
    Log.start()
    waiting_room_pid = WaitingRoom.start()
    barber_pid = Barber.start(waiting_room_pid)
    Log.log("ShopOwner", "barbershop open")
    Process.sleep(2000)
    spawn_customers(waiting_room_pid, 1, ShopConfig.customers())
    Log.log("ShopOwner", "waiting #{ShopConfig.grace_period()}ms")
    Process.sleep(ShopConfig.grace_period())
    Log.log("ShopOwner", "shutting down")
    send(barber_pid, {:get_stats, self()})
    send(waiting_room_pid, {:get_stats, self()})
    {cuts, avg_duration, avg_rating} =
      receive do
        {:barber_stats, cuts, avg_duration, avg_rating} -> {cuts, avg_duration, avg_rating}
      end
    {turned_away, _queue_len} =
      receive do
        {:wr_stats, turned_away, queue_len} -> {turned_away, queue_len}
      end
    send(barber_pid, :shutdown)
    send(waiting_room_pid, :shutdown)
    Process.sleep(200)
    IO.puts("")
    IO.puts("=== Barbershop Closing Report ===")
    IO.puts("Total customers arrived:    #{ShopConfig.customers()}")
    IO.puts("Customers served:           #{cuts}")
    IO.puts("Customers turned away:      #{turned_away}")
    IO.puts("Average haircut duration:   #{Float.round(avg_duration / 1000.0, 2)}s")
    IO.puts("Average satisfaction:        #{Float.round(avg_rating, 1)} / 5.0")
    IO.puts("=================================")
  end
  defp spawn_customers(_waiting_room_pid, current, total) when current > total do
    :ok
  end
  defp spawn_customers(waiting_room_pid, current, total) do
    Log.log("ShopOwner", "spawning Customer #{current} of #{total}")
    Customer.start(current, waiting_room_pid)
    Process.sleep(Enum.random(ShopConfig.arrival_min()..ShopConfig.arrival_max()))
    spawn_customers(waiting_room_pid, current + 1, total)
  end
end

defmodule Log do
  def start do
    :persistent_term.put(:log_start, System.monotonic_time(:millisecond))
  end
  def log(entity, msg) do
    start = :persistent_term.get(:log_start)
    elapsed = System.monotonic_time(:millisecond) - start
    IO.puts("(#{elapsed}ms) (#{entity}) #{msg}")
  end
end

defmodule Customer do
  def start(id, waiting_room_pid) do
    spawn(fn -> run(id, waiting_room_pid) end)
  end
  defp run(id, waiting_room_pid) do
    arrival_time = System.monotonic_time(:millisecond)
    Log.log("Customer #{id}", "arrived")
    send(waiting_room_pid, {:arrive, self(), id})
    receive do
      :turned_away ->
        Log.log("Customer #{id}", "turned away")

      :admitted ->
        Log.log("Customer #{id}", "admitted to waiting room")
        receive do
          {:called_by_barber, _barber_pid} ->
            called_time = System.monotonic_time(:millisecond)
            wait_ms = called_time - arrival_time
            receive do
              {:rate_req, barber_pid2} ->
                wait_seconds = wait_ms / 1000.0
                score = 5 - trunc(wait_seconds / ShopConfig.satisfaction()) + Enum.random(-1..1)
                score = max(1, min(5, score))
                Log.log("Customer #{id}", "giving rating #{score} (waited #{Float.round(wait_seconds, 2)}s)")
                send(barber_pid2, {:rating, id, score})
            end
        end
    end
  end
end

defmodule WaitingRoom do
  def start do
    spawn(fn -> loop([], 0, false, nil) end)
  end
  defp loop(q, turned_away, sleeping_flag, barber_pid) do
    receive do
      {:arrive, customer_pid, customer_id} ->
        if length(q) >= ShopConfig.capacity() do
          send(customer_pid, :turned_away)
          Log.log("WaitingRoom", "turned away Customer #{customer_id} (#{length(q)}/#{ShopConfig.capacity()})")
          loop(q, turned_away + 1, sleeping_flag, barber_pid)
        else
          send(customer_pid, :admitted)
          new_q = q ++ [{customer_pid, customer_id}]
          Log.log("WaitingRoom", "admitted Customer #{customer_id} (#{length(new_q)}/#{ShopConfig.capacity()})")
          if sleeping_flag do
            Log.log("WaitingRoom", "waking up the barber")
            send(barber_pid, :wakeup)
            loop(new_q, turned_away, false, barber_pid)
          else
            loop(new_q, turned_away, sleeping_flag, barber_pid)
          end
        end
      {:next_customer, from_barber_pid} ->
        case q do
          [{customer_pid, customer_id} | rest] ->
            Log.log("WaitingRoom", "sending Customer #{customer_id} to barber (q: #{length(rest)}/#{ShopConfig.capacity()})")
            send(from_barber_pid, {:customer_ready, customer_pid, customer_id})
            loop(rest, turned_away, false, from_barber_pid)

          [] ->
            Log.log("WaitingRoom", "no customers waiting barber will sleep")
            send(from_barber_pid, :none_waiting)
            loop([], turned_away, true, from_barber_pid)
        end
      {:get_stats, from} ->
        send(from, {:wr_stats, turned_away, length(q)})
        loop(q, turned_away, sleeping_flag, barber_pid)
      :shutdown ->
        Log.log("WaitingRoom", "shutting down")
    end
  end
end

defmodule Barber do
  def start(waiting_room_pid) do
    spawn(fn ->
      send(waiting_room_pid, {:next_customer, self()})
      loop(waiting_room_pid, 0, 0.0, 0.0)
    end)
  end
  defp loop(waiting_room_pid, cuts, avg_duration, avg_rating) do
    receive do
      {:customer_ready, customer_pid, customer_id} ->
        duration = Enum.random(ShopConfig.duration_min()..ShopConfig.duration_max())
        Log.log("Barber", "starting haircut for Customer #{customer_id} (#{duration}ms)")
        send(customer_pid, {:called_by_barber, self()})
        Process.sleep(duration)
        Log.log("Barber", "finished haircut for Customer #{customer_id}")
        send(customer_pid, {:rate_req, self()})
        receive do
          {:rating, customer_id, score} ->
            new_cuts = cuts + 1
            new_avg_duration = avg_duration + (duration - avg_duration) / new_cuts
            new_avg_rating = avg_rating + (score - avg_rating) / new_cuts
            Log.log("Barber", "received rating #{score} from Customer #{customer_id} avg duration: #{Float.round(new_avg_duration, 0)}ms, avg rating: #{Float.round(new_avg_rating, 2)}")
            send(waiting_room_pid, {:next_customer, self()})
            loop(waiting_room_pid, new_cuts, new_avg_duration, new_avg_rating)
        end
      :none_waiting ->
        Log.log("Barber", "no customers going to sleep")
        sleep_loop(waiting_room_pid, cuts, avg_duration, avg_rating)
      {:get_stats, from} ->
        send(from, {:barber_stats, cuts, avg_duration, avg_rating})
        loop(waiting_room_pid, cuts, avg_duration, avg_rating)
      :shutdown ->
        Log.log("Barber", "shutting down")
    end
  end
  defp sleep_loop(waiting_room_pid, cuts, avg_duration, avg_rating) do
    receive do
      :wakeup ->
        Log.log("Barber", "woke up")
        send(waiting_room_pid, {:next_customer, self()})
        loop(waiting_room_pid, cuts, avg_duration, avg_rating)
      {:get_stats, from} ->
        send(from, {:barber_stats, cuts, avg_duration, avg_rating})
        sleep_loop(waiting_room_pid, cuts, avg_duration, avg_rating)
      :shutdown ->
        Log.log("Barber", "shutting down")
    end
  end
end

ShopOwner.start()
