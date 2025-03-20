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
    <title>OCP Test Results Dashboard</title>
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
        .dashboard {
            margin-bottom: 50px;
            padding: 20px;
            background: #ffffff;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
            border-radius: 8px;
        }
        table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 20px;
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
        td:hover {
            background-color: #f1f1f1;
        }
        .status-btn {
            padding: 6px 12px;
            border-radius: 5px;
            cursor: pointer;
            font-weight: bold;
            border: none;
            display: inline-block;
            text-align: center;
            text-decoration: none;
        }
        .success { background-color: #28a745; color: white; }
        .failure { background-color: #dc3545; color: white; }
        .aborted { background-color: #ffc107; color: black; }
        .unknown { background-color: #6c757d; color: white; }
        .status-btn:hover {
            opacity: 0.9;
        }
        .status-btn:active {
            transform: scale(0.98);
        }
        .filter-container {
            text-align: center;
            margin-bottom: 20px;
        }
        .filter-container input {
            padding: 8px 12px;
            margin: 5px;
            border-radius: 5px;
            border: 1px solid #ddd;
        }
        @media (max-width: 768px) {
            table, th, td {
                font-size: 14px;
            }
            .status-btn {
                font-size: 12px;
                padding: 4px 8px;
            }
        }
    </style>
</head>
<body>
    <h2>OCP Test Results Dashboard</h2>

    <!-- Filter Section -->
    <div class="filter-container">
        <input type="text" id="searchOcp" placeholder="Search by OCP Version" onkeyup="filterTable()">
        <input type="text" id="searchGpu" placeholder="Search by GPU Version" onkeyup="filterTable()">
        <input type="text" id="searchStatus" placeholder="Search by Status" onkeyup="filterTable()">
    </div>

    <!-- Dashboard 1: Passed Tests -->
    <div class="dashboard">
        <h3>✅ Passed Tests</h3>
        <table id="passedTestsTable">
            <thead>
                <tr>
                    <th>OCP Version</th>
                    <th>GPU Version</th>
                    <th>Status</th>
                </tr>
            </thead>
            <tbody>
"""

# Add passed tests
for ocp_version in sorted_ocp_versions:
    for result in ocp_data[ocp_version]:
        if result["status"] == "SUCCESS":
            html_content += f"""
            <tr onclick="window.open('{result['link']}', '_blank')">
                <td>{result["ocp"]}</td>
                <td>{result["gpu"]}</td>
                <td><a href="{result['link']}" target="_blank" class='status-btn success'>Test Passed</a></td>
            </tr>
            """

html_content += """
            </tbody>
        </table>
    </div>

    <!-- Dashboard 2: Failed & Aborted Tests -->
    <div class="dashboard">
        <h3>❌ Failed & Aborted Tests</h3>
        <table id="failedTestsTable">
            <thead>
                <tr>
                    <th>OCP Version</th>
                    <th>GPU Version</th>
                    <th>Status</th>
                </tr>
            </thead>
            <tbody>
"""

# Add failed & aborted tests
for ocp_version in sorted_ocp_versions:
    for result in ocp_data[ocp_version]:
        if result["status"] in ["FAILURE", "ABORTED"]:
            status_class = "failure" if result["status"] == "FAILURE" else "aborted"
            html_content += f"""
            <tr onclick="window.open('{result['link']}', '_blank')">
                <td>{result["ocp"]}</td>
                <td>{result["gpu"]}</td>
                <td><a href="{result['link']}" target="_blank" class='status-btn {status_class}'>{result["status"]}</a></td>
            </tr>
            """

html_content += """
            </tbody>
        </table>
    </div>

    <script>
        // Filter function to search by OCP, GPU, or Status
        function filterTable() {
            const searchOcp = document.getElementById("searchOcp").value.toLowerCase();
            const searchGpu = document.getElementById("searchGpu").value.toLowerCase();
            const searchStatus = document.getElementById("searchStatus").value.toLowerCase();
            
            const tables = [document.getElementById("passedTestsTable"), document.getElementById("failedTestsTable")];
            tables.forEach(table => {
                const rows = table.getElementsByTagName("tr");
                for (let i = 1; i < rows.length; i++) {
                    const row = rows[i];
                    const ocp = row.getElementsByTagName("td")[0].textContent.toLowerCase();
                    const gpu = row.getElementsByTagName("td")[1].textContent.toLowerCase();
                    const status = row.getElementsByTagName("td")[2].textContent.toLowerCase();
                    if (
                        ocp.includes(searchOcp) &&
                        gpu.includes(searchGpu) &&
                        status.includes(searchStatus)
                    ) {
                        row.style.display = "";
                    } else {
                        row.style.display = "none";
                    }
                }
            });
        }
    </script>
</body>
</html>
"""

os.makedirs("output", exist_ok=True)
# Save the HTML file
with open("output/v7_report.html", "w") as f:
    f.write(html_content)

print("Dual dashboard report generated: v7_report.html")
