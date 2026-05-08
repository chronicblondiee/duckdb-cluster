-- wrk script for single document writes
-- Usage: wrk -t4 -c20 -d30s --latency -s write-single.lua http://localhost:8091/indices/stress-test/_doc

-- Global counter for unique document IDs
local counter = 0
local thread_id = 0

-- Initialize thread-local variables
function setup(thread)
    thread:set("id", thread_id)
    thread_id = thread_id + 1
end

-- Initialize per-thread request counter
function init(args)
    counter = id * 1000000
end

-- Generate request for each iteration
request = function()
    counter = counter + 1
    
    local body = string.format([[{
  "id": %d,
  "value": %d,
  "category": "cat_%d",
  "timestamp": "%s",
  "message": "Load test document %d"
}]], counter, math.random(1, 1000), math.random(1, 10), 
    os.date("!%Y-%m-%dT%H:%M:%SZ"), counter)
    
    return wrk.format("POST", nil, {
        ["Content-Type"] = "application/json",
        ["Authorization"] = "Bearer " .. os.getenv("AUTH_TOKEN")
    }, body)
end

-- Track response statistics
response = function(status, headers, body)
    if status ~= 200 and status ~= 201 then
        print("Error status: " .. status .. " body: " .. body)
    end
end
