import json

# Load the JSON data
with open("ocp_data.json", "r") as f:
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
        .unknown { background-color: #fff3cd; color: #856404; }
        .history-bar {
            display: flex;
            justify-content: space-between;
            width: 160px;
        }
        .history-dot {
            width: 12px;
            height: 12px;
            border-radius: 50%;
        }
        .history-success { background-color: #28a745; }
        .history-failure { background-color: #dc3545; }
        .history-unknown { background-color: #ffc107; }
        .search-bar {
            margin-bottom: 20px;
            padding: 10px;
            width: 100%;
            max-width: 300px;
            margin-left: auto;
            margin-right: auto;
            border: 1px solid #ddd;
            border-radius: 5px;
            font-size: 14px;
        }
        .search-bar:focus {
            border-color: #007BFF;
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

        // Function to filter results based on search
        function filterTable() {
            let input = document.getElementById("searchInput");
            let filter = input.value.toLowerCase();
            let tables = document.getElementsByTagName("table");

            for (let table of tables) {
                let rows = table.getElementsByTagName("tr");
                for (let i = 1; i < rows.length; i++) {
                    let cells = rows[i].getElementsByTagName("td");
                    let match = false;
                    for (let cell of cells) {
                        if (cell.innerHTML.toLowerCase().includes(filter)) {
                            match = true;
                            break;
                        }
                    }
                    rows[i].style.display = match ? "" : "none";
                }
            }
        }
    </script>
</head>
<body>

    <h2>OCP Test Results Matrix</h2>
    <input type="text" id="searchInput" class="search-bar" onkeyup="filterTable()" placeholder="Search for results...">

"""

# Generate matrix for each OCP version
for ocp_version in sorted_ocp_versions:
    results = ocp_data[ocp_version]
    html_content += f"""
    <div class="ocp-version-container">
        <div class="ocp-version-header">OCP Version {ocp_version}</div>
        <table id="table-{ocp_version}">
            <thead>
                <tr>
                    <th onclick="sortTable(0, 'table-{ocp_version}')">Full OCP Version</th>
                    <th onclick="sortTable(1, 'table-{ocp_version}')">GPU Version</th>
                    <th onclick="sortTable(2, 'table-{ocp_version}')">Status</th>
                    <th>History of the Last 15 Tests</th>
                </tr>
            </thead>
            <tbody>
    """
    
    # Populate rows for each test result
    for result in results:
        full_ocp = result["ocp"]
        gpu_version = result["gpu"]
        status = result["status"]
        link = result["link"]
        
        # Determine row color
        status_class = "success" if status == "SUCCESS" else "failure" if status == "FAILURE" else "unknown"
        
        # Create history dots
        history = result.get("history", [])
        history_dots = "".join([
            f"<div class='history-dot { 'history-success' if h == 'SUCCESS' else 'history-failure' if h == 'FAILURE' else 'history-unknown' }'></div>"
            for h in history
        ])
        
        # Add row
        html_content += f"""
        <tr>
            <td>{full_ocp}</td>
            <td>{gpu_version}</td>
            <td><button class="status-btn {status_class}" onclick="window.open('{link}', '_blank')">{status}</button></td>
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
with open("v5_report.html", "w") as f:
    f.write(html_content)

print("Matrix report generated: ocp_report_matrix.html")
