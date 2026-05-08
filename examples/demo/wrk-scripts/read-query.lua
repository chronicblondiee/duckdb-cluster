-- wrk script for read queries
-- Usage: wrk -t4 -c20 -d30s --latency -s read-query.lua http://localhost:8091/query

-- Define a set of test queries
local queries = {
    '{"sql":"SELECT COUNT(*) FROM _docs","index":"stress-test"}',
    '{"sql":"SELECT * FROM _docs LIMIT 10","index":"stress-test"}',
    '{"sql":"SELECT AVG(value) FROM _docs","index":"stress-test"}',
    '{"sql":"SELECT category, COUNT(*) AS cnt FROM _docs GROUP BY category","index":"stress-test"}',
    '{"sql":"SELECT * FROM _docs WHERE value > 500 LIMIT 20","index":"stress-test"}',
    '{"sql":"SELECT category, AVG(value) AS avg_val FROM _docs GROUP BY category ORDER BY avg_val DESC","index":"stress-test"}',
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
        print("Error status: " .. status .. " body: " .. body)
    end
end
