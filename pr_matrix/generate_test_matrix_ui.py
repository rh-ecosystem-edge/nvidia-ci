import json

# Load the JSON data
with open("output/ocp_data.json", "r") as f:
    ocp_data = json.load(f)

# Sort OCP versions in descending order (newest to oldest)
sorted_ocp_versions = sorted(ocp_data.keys(), reverse=True)

# Define HTML structure
html_content = """
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>OCP Test Results Matrix</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            background-color: #f4f7fc;
            margin: 0;
            padding: 20px;
            color: #333;
        }
        h2 {
            text-align: center;
            margin-bottom: 20px;
            color: #007bff;
        }
        .ocp-version-container {
            margin-bottom: 40px;
        }
        .ocp-version-header {
            font-size: 24px;
            margin-bottom: 10px;
            color: #333;
            background-color: #e9ecef;
            padding: 10px;
            border-radius: 5px;
        }
        table {
            width: 100%;
            border-collapse: collapse;
            margin: 20px 0;
            background-color: #ffffff;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
            border-radius: 8px;
        }
        th, td {
            border: 1px solid #ddd;
            padding: 12px;
            text-align: left;
        }
        th {
            background-color: #007BFF;
            color: white;
            cursor: pointer;
        }
        th:hover {
            background-color: #0056b3;
        }
        td {
            background-color: #f9f9f9;
        }
        td:hover {
            background-color: #f1f1f1;
        }
        .status-btn {
            padding: 6px 12px;
            border-radius: 5px;
            cursor: pointer;
            font-weight: bold;
            border: none;
        }
        .success { background-color: #d4edda; color: #155724; }
        .failure { background-color: #f8d7da; color: #721c24; }
        .aborted { background-color: #fff3cd; color: #856404; }
        .history-bar {
            display: flex;
            gap: 5px;
            align-items: center;
        }
        .history-dot {
            width: 20px;
            height: 20px;
            border-radius: 50%;
            cursor: pointer;
        }
        .history-success {
            background-color: green;
        }
        .history-failure {
            background-color: red;
        }
        .history-aborted {
            background-color: yellow;
        }
    </style>
    <script>
        // Function to sort the table
        function sortTable(n, tableId) {
            let table = document.getElementById(tableId);
            let rows = table.rows;
            let switching = true;
            let shouldSwitch, i, x, y;
            let dir = "asc"; // Default direction

            while (switching) {
                switching = false;
                for (i = 1; i < (rows.length - 1); i++) {
                    shouldSwitch = false;
                    x = rows[i].getElementsByTagName("TD")[n];
                    y = rows[i + 1].getElementsByTagName("TD")[n];
                    if (dir === "asc") {
                        if (x.innerHTML.toLowerCase() > y.innerHTML.toLowerCase()) {
                            shouldSwitch = true;
                            break;
                        }
                    } else if (dir === "desc") {
                        if (x.innerHTML.toLowerCase() < y.innerHTML.toLowerCase()) {
                            shouldSwitch = true;
                            break;
                        }
                    }
                }
                if (shouldSwitch) {
                    rows[i].parentNode.insertBefore(rows[i + 1], rows[i]);
                    switching = true;
                } else {
                    if (dir === "asc") {
                        dir = "desc";
                    } else {
                        break;
                    }
                }
            }
        }
    </script>
</head>
<body>

<h2>OCP Test Results Matrix</h2>

"""

# Generate matrix for each OCP version
for ocp_version in sorted_ocp_versions:
    results = ocp_data[ocp_version]
    
    # Separate bundle and regular results
    bundle_results = [
        result for result in results 
        if "bundle" in result["gpu"].lower() or "master" in result["gpu"].lower()
    ]

    # Regular results will be everything that is not in bundle_results
    regular_results = [
        result for result in results 
        if "bundle" not in result["gpu"].lower() and "master" not in result["gpu"].lower()
    ]

    # Regular Results Table
    html_content += f"""
    <div class="ocp-version-container">
        <div class="ocp-version-header">OCP Version {ocp_version} (Regular)</div>
        <table id="table-{ocp_version}-regular">
            <thead>
                <tr>
                    <th onclick="sortTable(0, 'table-{ocp_version}-regular')">Full OCP Version</th>
                    <th onclick="sortTable(1, 'table-{ocp_version}-regular')">GPU Version</th>
                    <th>Link to Job</th>
                </tr>
            </thead>
            <tbody>
    """
    
    # Populate rows for regular test results
    for result in regular_results:
        full_ocp = result["ocp"]
        gpu_version = result["gpu"]
        link = result["link"]
        
        html_content += f"""
        <tr>
            <td>{full_ocp}</td>
            <td>{gpu_version}</td>
            <td><a href="{link}" target="_blank">Job Link</a></td>
        </tr>
        """
    
    html_content += """
            </tbody>
        </table>
    </div>
    """

    # Bundle Results Table
    html_content += f"""
    <div class="ocp-version-container">
        <div class="ocp-version-header">OCP Version {ocp_version} (Bundle)</div>
        <table id="table-{ocp_version}-bundle">
            <thead>
                <tr>
                    <th onclick="sortTable(0, 'table-{ocp_version}-bundle')">GPU Version</th>
                    <th>Last Finished</th>
                    <th>History of the Last 15 Tests</th>
                </tr>
            </thead>
            <tbody>
    """
    
    # Populate rows for bundle test results
    for result in bundle_results:
        gpu_version = result["gpu"]
        timestamp = result["timestamp"]
        # Format the timestamp into a readable date (optional)
        from datetime import datetime
        from datetime import timezone

        formatted_time = datetime.fromtimestamp(timestamp, tz=timezone.utc).strftime('%Y-%m-%d %H:%M:%S')


        history = result.get("history", [])
        history_dots = "".join([
            f"<div class='history-dot { 'history-success' if h == 'SUCCESS' else 'history-failure' if h == 'FAILURE' else 'history-aborted' }' onclick='window.open(\"{result['link']}\", \"_blank\")'></div>"
            for h in history
        ])
        
        html_content += f"""
        <tr>
            <td>{gpu_version}</td>
            <td>{formatted_time}</td>
            <td><div class='history-bar'>{history_dots}</div></td>
        </tr>
        """

    
    html_content += """
            </tbody>
        </table>
    </div>
    """

# Close HTML tags
html_content += """
</body>
</html>
"""

# Save the HTML file
with open("output/index.html", "w") as f:
    f.write(html_content)

print("Matrix report generated: output/index.html")
