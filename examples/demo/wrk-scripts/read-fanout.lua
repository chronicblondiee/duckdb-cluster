-- wrk script for cross-index fan-out queries
-- Usage: wrk -t4 -c20 -d30s --latency -s read-fanout.lua http://localhost:8091/query

-- Queries across multiple indices
local queries = {
    '{"sql":"SELECT COUNT(*) FROM _docs","index":"fanout-1"}',
    '{"sql":"SELECT COUNT(*) FROM _docs","index":"fanout-2"}',
    '{"sql":"SELECT COUNT(*) FROM _docs","index":"fanout-3"}',
    '{"sql":"SELECT * FROM _docs LIMIT 5","index":"fanout-1"}',
    '{"sql":"SELECT * FROM _docs LIMIT 5","index":"fanout-2"}',
    '{"sql":"SELECT * FROM _docs LIMIT 5","index":"fanout-3"}',
    '{"sql":"SELECT AVG(value) FROM _docs","index":"fanout-1"}',
    '{"sql":"SELECT AVG(value) FROM _docs","index":"fanout-2"}',
    '{"sql":"SELECT AVG(value) FROM _docs","index":"fanout-3"}',
}

local counter = 0

request = function()
    counter = counter + 1
    local query = queries[(counter % #queries) + 1]
    
    return wrk.format("POST", nil, {
        ["Content-Type"] = "application/json",
        ["Authorization"] = "Bearer " .. os.getenv("AUTH_TOKEN")
    }, query)
end

response = function(status, headers, body)
    if status ~= 200 then
        print("Error status: " .. status)
    end
end
