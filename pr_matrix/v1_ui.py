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
        body { font-family: Arial, sans-serif; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
        .success { background-color: #c8e6c9; } /* Green */
        .failure { background-color: #ffcccb; } /* Red */
        .unknown { background-color: #fff3cd; } /* Yellow */
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
            <th>Link</th>
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
        
        # Add row
        html_content += f"""
        <tr class='{status_class}'>
            <td>{ocp_version}</td>
            <td>{full_ocp}</td>
            <td>{gpu_version}</td>
            <td>{status}</td>
            <td><a href='{link}' target='_blank'>View</a></td>
        </tr>
        """

# Close HTML tags
html_content += """
    </table>
</body>
</html>
"""

# Save the HTML file
with open("v1_report.html", "w") as f:
    f.write(html_content)

print("Report generated: ocp_report.html")
