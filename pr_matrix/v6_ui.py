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
            text-decoration: none;
            display: inline-block;
            text-align: center;
        }
        .success { background-color: #d4edda; color: #155724; }
        .failure { background-color: #f8d7da; color: #721c24; }
        .unknown { background-color: #fff3cd; color: #856404; }
    </style>
</head>
<body>
    <h2>OCP Test Results Dashboard</h2>
    
    <!-- Dashboard 1: Passed Tests -->
    <div class="dashboard">
        <h3>✅ Passed Tests</h3>
        <table>
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
            <tr>
                <td>{result["ocp"]}</td>
                <td>{result["gpu"]}</td>
                <td><a href="{result['link']}" target="_blank" class='status-btn success'>SUCCESS</a></td>
            </tr>
            """

html_content += """
            </tbody>
        </table>
    </div>

    <!-- Dashboard 2: Failed & Aborted Tests -->
    <div class="dashboard">
        <h3>❌ Failed & Aborted Tests</h3>
        <table>
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
            status_class = "failure" if result["status"] == "FAILURE" else "unknown"
            html_content += f"""
            <tr>
                <td>{result["ocp"]}</td>
                <td>{result["gpu"]}</td>
                <td><a href="{result['link']}" target="_blank" class='status-btn {status_class}'>{result["status"]}</a></td>
            </tr>
            """

html_content += """
            </tbody>
        </table>
    </div>
</body>
</html>
"""

# Save the HTML file
with open("v6_report.html", "w") as f:
    f.write(html_content)

print("Dual dashboard report generated: v6_report.html")
