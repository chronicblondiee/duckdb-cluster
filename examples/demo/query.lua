-- wrk Lua script for DuckDB Cluster query endpoint
-- Usage: wrk -t4 -c20 -d10s --latency -s query.lua http://localhost:8080/query

wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"

-- Get auth token from environment
local auth_token = os.getenv("AUTH_TOKEN")
if auth_token then
    wrk.headers["Authorization"] = "Bearer " .. auth_token
end

-- Query payload
wrk.body = '{"sql":"SELECT * FROM _docs LIMIT 10","index":"stress-test"}'

-- Optional: rotate through multiple queries
local queries = {
    '{"sql":"SELECT * FROM _docs LIMIT 10","index":"stress-test"}',
    '{"sql":"SELECT COUNT(*) FROM _docs","index":"stress-test"}',
    '{"sql":"SELECT category, COUNT(*) AS cnt FROM _docs GROUP BY category","index":"stress-test"}',
}

local counter = 0

request = function()
    counter = counter + 1
    local query = queries[(counter % #queries) + 1]
    return wrk.format("POST", nil, wrk.headers, query)
end

-- Report statistics
done = function(summary, latency, requests)
    io.write("------------------------------\n")
    io.write(string.format("  Requests:      %d\n", summary.requests))
    io.write(string.format("  Duration:      %.2fs\n", summary.duration / 1000000))
    io.write(string.format("  Requests/sec:  %.2f\n", summary.requests / (summary.duration / 1000000)))
    io.write(string.format("  Transfer:      %.2f KB\n", summary.bytes / 1024))
    io.write("------------------------------\n")
    io.write("Latency Distribution:\n")
    io.write(string.format("  50%%:  %.3fms\n", latency:percentile(50)))
    io.write(string.format("  75%%:  %.3fms\n", latency:percentile(75)))
    io.write(string.format("  90%%:  %.3fms\n", latency:percentile(90)))
    io.write(string.format("  95%%:  %.3fms\n", latency:percentile(95)))
    io.write(string.format("  99%%:  %.3fms\n", latency:percentile(99)))
    io.write(string.format("  Max:   %.3fms\n", latency.max))
    io.write("------------------------------\n")
end
