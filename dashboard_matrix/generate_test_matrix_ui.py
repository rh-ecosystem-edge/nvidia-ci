import json
from store_data import logger
import os
from datetime import datetime, timezone

# HTML Header with Updated Styling for User-Friendliness and Modern Colors
def generate_html_header():
    return """
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>Test Matrix: NVIDIA GPU Operator on Red Hat OpenShift</title>
        <style>
            body {
                font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
                background-color: #f1f1f1; /* Soft background */
                margin: 0;
                padding: 20px;
                color: #333;
            }
            h2 {
                text-align: center;
                margin-bottom: 20px;
                color: #007bff; /* Soft blue */
                font-size: 28px;
            }
            .ocp-version-container {
                margin-bottom: 40px;
                padding: 20px;
                background-color: #ffffff;
                border-radius: 8px;
                box-shadow: 0 4px 8px rgba(0, 0, 0, 0.1);
            }
            .ocp-version-header {
                font-size: 26px;
                margin-bottom: 15px;
                color: #333;
                background-color: #f7f9fc; /* Light grey background */
                padding: 15px;
                border-radius: 8px;
                font-weight: bold;
            }
            table {
                width: 100%;
                border-collapse: collapse;
                margin: 20px 0;
                background-color: #ffffff;
                box-shadow: 0 4px 8px rgba(0, 0, 0, 0.1);
                border-radius: 8px;
            }
            th, td {
                border: 1px solid #ddd;
                padding: 12px;
                text-align: left;
                font-size: 14px;
                transition: background-color 0.2s ease;
            }
            th {
                background-color: #007BFF; /* Blue */
                color: white;
                cursor: pointer;
                font-size: 16px;
                position: relative;
            }
            th:hover {
                background-color: #0056b3; /* Darker blue */
            }
            th:after {
                content: ' ▼'; /* Default sorting direction */
                position: absolute;
                right: 10px;
                font-size: 12px;
                color: white;
            }
            th.asc:after {
                content: ' ▲';
            }
            th.desc:after {
                content: ' ▼';
            }
            td {
                background-color: #f9f9f9;
            }
            td:hover {
                background-color: #f1f1f1;
                cursor: pointer;
            }

            /* History bar styles */
            .history-bar {
                display: flex;
                align-items: center;
                gap: 20px;
                margin: 20px 0;
                padding: 12px 18px;
                border: 2px solid #007BFF;
                border-radius: 8px;
                background-color: #ffffff;
                color: #333;
                box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
                transition: background-color 0.2s ease;
                flex-wrap: wrap;
            }

            .history-square {
                width: 55px;
                height: 55px;
                border-radius: 8px;
                cursor: pointer;
                transition: transform 0.1s ease;
                border: 2px solid #ddd;
                position: relative;
                overflow: hidden;
                box-shadow: 0 2px 6px rgba(0, 0, 0, 0.1);
            }

            .history-square:hover {
                transform: scale(1.2);
                box-shadow: 0 0 8px rgba(0, 0, 0, 0.2);
            }

            /* Status coloring */
            .history-success {
                background-color: #64dd17; /* Bright green */
            }

            .history-failure {
                background-color: #ff3d00; /* Bright red */
            }

            .history-aborted {
                background-color: #ffd600; /* Yellow */
            }

            /* Tooltip on hover */
            .history-square:hover::after {
                content: attr(title);  /* Display status and timestamp as tooltip */
                position: absolute;
                background-color: #333;
                color: white;
                padding: 8px 12px;
                border-radius: 5px;
                font-size: 12px;
                top: 60px;
                z-index: 10;
                width: 200px;
                text-align: center;
            }

            /* Responsive design for smaller screens */
            @media screen and (max-width: 768px) {
                table {
                    font-size: 14px;
                }
                .history-bar {
                    flex-wrap: wrap;
                    justify-content: center;
                }
                .history-square {
                    width: 45px;
                    height: 45px;
                }
            }
        </style>
    </head>
    <body>

    <h2>Test Matrix: NVIDIA GPU Operator on Red Hat OpenShift</h2>
    <script>
        // Function to sort the table
        function sortTable(column, tableId) {
            var table = document.getElementById(tableId);
            var rows = Array.from(table.rows);
            var isAscending = table.rows[0].cells[column].classList.contains('asc');
            rows = rows.slice(1);  // Exclude the header row

            rows.sort(function(rowA, rowB) {
                var cellA = rowA.cells[column].innerText;
                var cellB = rowB.cells[column].innerText;

                // Handle numeric and string sorting
                if (!isNaN(cellA) && !isNaN(cellB)) {
                    return isAscending ? cellA - cellB : cellB - cellA;
                } else {
                    return isAscending
                        ? cellA.localeCompare(cellB)
                        : cellB.localeCompare(cellA);
                }
            });

            // Rebuild table rows
            rows.forEach(function(row) {
                table.appendChild(row);
            });

            // Toggle the sorting direction
            var header = table.rows[0].cells[column];
            header.classList.toggle('asc', !isAscending);
            header.classList.toggle('desc', isAscending);
        }
    </script>
    """

# Generate the regular results table with bundled results in a row
def generate_regular_results_table(ocp_version, regular_results, bundle_results):
    table_html = f"""
    <div class="ocp-version-container">
        <div class="ocp-version-header">OpenShift {ocp_version}</div>
        <div><strong>Operator Catalog</strong></div>
        <table id="table-{ocp_version}-regular">
            <thead>
                <tr>
                    <th onclick="sortTable(0, 'table-{ocp_version}-regular')">Full OCP Version</th>
                    <th onclick="sortTable(1, 'table-{ocp_version}-regular')">GPU Version</th>
                    <th>Prow Job</th>
                </tr>
            </thead>
            <tbody>
    """
    
    # Populate rows for regular test results
    for result in regular_results:
        full_ocp = result["ocp"]
        gpu_version = result["gpu"]
        link = result["link"]
        
        table_html += f"""
        <tr>
            <td>{full_ocp}</td>
            <td>{gpu_version}</td>
            <td><a href="{link}" target="_blank">Job Link</a></td>
        </tr>
        """
    
    table_html += """
            </tbody>
        </table>
    """

    # Add the bundle results below the regular table in a row
    if bundle_results:
        table_html += """
        <div><strong>Bundles Associated with this Regular Results</strong></div>
        <div style="padding-left: 20px; display: flex; flex-wrap: wrap; gap: 15px;">
        """
        for bundle in bundle_results:
            status = bundle.get("status", "Unknown")
            status_class = "history-success" if status == "SUCCESS" else "history-failure" if status == "FAILURE" else "history-aborted"
            bundle_timestamp = datetime.utcfromtimestamp(bundle["timestamp"]).strftime("%Y-%m-%d %H:%M:%S UTC")
            table_html += f"""
            <div class='history-square {status_class}' 
                onclick='window.open("{bundle["link"]}", "_blank")' 
                title='Status: {status} | Timestamp: {bundle_timestamp}'>
            </div>
            """
        table_html += "</div>"
        # Display the date of the last bundle
        last_bundle_date = datetime.utcfromtimestamp(bundle_results[0]["timestamp"]).strftime("%Y-%m-%d %H:%M:%S UTC")
        table_html += f"""
        <div><strong>Last Bundle Job Date: </strong>{last_bundle_date}</div>
        """

    table_html += "</div>"
    
    return table_html

# Generate the bundle results section with clickable squares
def generate_bundle_results_section(bundle_results):
    if not bundle_results:
        print("No bundle results found.")
        return ""

    # Get the latest bundle
    latest_bundle = bundle_results[0]  # Always take the top of the main branch
    timestamp = latest_bundle.get("timestamp", "Unknown Timestamp")  # Assuming timestamp field exists
    
    # Convert timestamp to datetime in UTC and format it
    if timestamp != "Unknown Timestamp":
        dt = datetime.utcfromtimestamp(timestamp)
        timestamp = dt.replace(tzinfo=timezone.utc).astimezone(timezone.utc).strftime('%Y-%m-%d %H:%M:%S %Z')

    print(f"Latest bundle found: {latest_bundle}")
    print(f"Timestamp: {timestamp}")

    # Create the fake data for history bar (latest 15 results)
    fake_data = {
        "fake-version": bundle_results[:15]  # The latest 15 results
    }
    print(f"Fake data for history bar: {fake_data}")

    # Generate the history bar HTML with clickable status squares
    history_bar_html = "<div class='history-bar'>"
    for result in bundle_results[:15]:  # Limit to latest 15 results
        status = result.get("status", "Unknown")  # Get the status (e.g., SUCCESS, FAILURE, ABORTED)
        status_class = "history-success" if status == "SUCCESS" else "history-failure" if status == "FAILURE" else "history-aborted"

        # Prepare status and timestamp info for tooltip
        status_info = f"Status: {status} | Timestamp: {datetime.utcfromtimestamp(result['timestamp']).strftime('%Y-%m-%d %H:%M:%S UTC')}"

        history_bar_html += f"""
        <div class='history-square {status_class}' 
            onclick='window.open("{result["link"]}", "_blank")' 
            title='{status_info}'>
        </div>
        """
    
    history_bar_html += "</div>"
    print(f"History bar HTML: {history_bar_html}")

    # Generate the HTML section for the bundle results
    bundle_html = f"""
    <div><strong>Bundle from main branch</strong></div>
    <div style="display: flex; justify-content: space-between; align-items: center;">
        <span style="font-weight: bold;">{timestamp}</span>
        <span>{history_bar_html}</span>
    </div>
    """
    print("Generated bundle results section HTML.")
    
    return bundle_html

# Generate the entire test matrix
def generate_test_matrix(ocp_data):
    sorted_ocp_versions = sorted(ocp_data.keys(), reverse=True)

    # Start with the HTML header
    html_content = generate_html_header()

    for ocp_version in sorted_ocp_versions:
        results = ocp_data[ocp_version]
        
        # First, define regular_results strictly:
        regular_results = [
            r for r in results
            if ("bundle" not in r["gpu"].lower())
            and ("master" not in r["gpu"].lower())
            and (r.get("status") == "SUCCESS")
        ]

        # Then, let bundle_results be everything else:
        bundle_results = [
            r for r in results
            if r not in regular_results
        ]

        
        # Add regular results table to the HTML, along with associated bundle results
        html_content += generate_regular_results_table(ocp_version, regular_results, bundle_results)

    # Close HTML tags
    html_content += """
    </body>
    </html>
    """

    return html_content



def test_generate_test_matrix():
    # Load the JSON data from dashboard_matrix/ocp_data.json
    with open("dashboard_matrix/ocp_data.json", "r") as f:
        ocp_data = json.load(f)

    # Generate the HTML content
    html_content = generate_test_matrix(ocp_data)

    # Save the HTML to dashboard_matrix/output (matching your GitHub Pages deploy folder)
    output_dir = "dashboard_matrix/output"
    os.makedirs(output_dir, exist_ok=True)  # Create the directory if it does not exist

    output_path = os.path.join(output_dir, "index.html")
    with open(output_path, "w") as f:
        f.write(html_content)

    logger.info(f"Matrix report generated: {output_path}")

# Run the test function to verify the changes
test_generate_test_matrix()
