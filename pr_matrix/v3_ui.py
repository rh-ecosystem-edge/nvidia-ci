import json

# Load the JSON data
with open("ocp_data.json", "r") as f:
    ocp_data = json.load(f)

# Define HTML structure
html_content = """
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>OCP Test Results</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; padding: 20px; background-color: #f9f9f9; }
        h2 { text-align: center; color: #333; }
        table { width: 100%; border-collapse: collapse; background: white; box-shadow: 0 0 10px rgba(0, 0, 0, 0.1); }
        th, td { border: 1px solid #ddd; padding: 12px; text-align: left; }
        th { background-color: #007BFF; color: white; }
        .status-btn { cursor: pointer; padding: 5px 10px; border-radius: 5px; font-weight: bold; }
        .success { background-color: #d4edda; color: #155724; }
        .failure { background-color: #f8d7da; color: #721c24; }
        .unknown { background-color: #fff3cd; color: #856404; }
        a { text-decoration: none; color: #007BFF; font-weight: bold; }
        a:hover { text-decoration: underline; }
        .history-bar { display: flex; justify-content: space-between; width: 160px; }
        .history-dot { width: 12px; height: 12px; border-radius: 50%; }
        .history-success { background-color: #28a745; }
        .history-failure { background-color: #dc3545; }
        .history-unknown { background-color: #ffc107; }
    </style>
</head>
<body>
    <h2>OCP Test Results</h2>
    <table>
        <tr>
            <th>OCP Version</th>
            <th>Full OCP Version</th>
            <th>GPU Version</th>
            <th>Status</th>
            <th>History of the Last 15 Tests</th>
        </tr>
"""

# Populate table rows
for ocp_version, results in ocp_data.items():
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
            <td>{ocp_version}</td>
            <td>{full_ocp}</td>
            <td>{gpu_version}</td>
            <td><button class="status-btn {status_class}" onclick="window.open('{link}', '_blank')">{status}</button></td>
            <td><div class='history-bar'>{history_dots}</div></td>
        </tr>
        """

# Close HTML tags
html_content += """
    </table>
</body>
</html>
"""

# Save the HTML file
with open("v3_report.html", "w") as f:
    f.write(html_content)

print("Report generated: ocp_report.html")
