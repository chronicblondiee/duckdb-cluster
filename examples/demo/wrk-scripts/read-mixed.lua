-- wrk script for mixed read patterns (simple + complex queries)
-- Usage: wrk -t4 -c20 -d30s --latency -s read-mixed.lua http://localhost:8091/query

local simple_queries = {
    '{"sql":"SELECT * FROM _docs LIMIT 5","index":"stress-test"}',
    '{"sql":"SELECT COUNT(*) FROM _docs","index":"stress-test"}',
    '{"sql":"SELECT * FROM _docs WHERE id > 1000 LIMIT 10","index":"stress-test"}',
}

local complex_queries = {
    '{"sql":"SELECT category, COUNT(*) AS cnt, AVG(value) AS avg FROM _docs GROUP BY category","index":"stress-test"}',
    '{"sql":"SELECT category, MIN(value), MAX(value), AVG(value) FROM _docs GROUP BY category HAVING COUNT(*) > 10","index":"stress-test"}',
    '{"sql":"SELECT * FROM _docs WHERE value BETWEEN 300 AND 700 ORDER BY value DESC LIMIT 50","index":"stress-test"}',
}

local counter = 0

request = function()
    counter = counter + 1
    
    -- 70% simple queries, 30% complex
    local query
    if counter % 10 < 7 then
        query = simple_queries[(counter % #simple_queries) + 1]
    else
        query = complex_queries[(counter % #complex_queries) + 1]
    end
    
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
