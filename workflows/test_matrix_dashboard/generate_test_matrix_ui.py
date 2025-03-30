import argparse
import json
import os
from datetime import datetime, timezone
from typing import Any, Dict, List


from logger import logger



def generate_html_header() -> str:
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
                background-color: #f1f1f1;
                margin: 0;
                padding: 20px;
                color: #333;
            }
            h2 {
                text-align: center;
                margin-bottom: 20px;
                color: #007bff;
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
                background-color: #f7f9fc;
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
                background-color: #007BFF;
                color: white;
                cursor: pointer;
                font-size: 16px;
                position: relative;
            }
            th:hover {
                background-color: #0056b3;
            }
            th:after {
                content: ' ▼';
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
            .history-success {
                background-color: #64dd17;
            }
            .history-failure {
                background-color: #ff3d00;
            }
            .history-aborted {
                background-color: #ffd600;
            }
            .history-square:hover::after {
                content: attr(title);
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
        function sortTable(column, tableId) {
            var table = document.getElementById(tableId);
            var rows = Array.from(table.rows);
            var isAscending = table.rows[0].cells[column].classList.contains('asc');
            rows = rows.slice(1);
            rows.sort(function(rowA, rowB) {
                var cellA = rowA.cells[column].innerText;
                var cellB = rowB.cells[column].innerText;
                if (!isNaN(cellA) && !isNaN(cellB)) {
                    return isAscending ? cellA - cellB : cellB - cellA;
                } else {
                    return isAscending ? cellA.localeCompare(cellB) : cellB.localeCompare(cellA);
                }
            });
            rows.forEach(function(row) {
                table.appendChild(row);
            });
            var header = table.rows[0].cells[column];
            header.classList.toggle('asc', !isAscending);
            header.classList.toggle('desc', isAscending);
        }
    </script>
    """

def generate_regular_results_table(ocp_version: str, regular_results: List[Dict[str, Any]], bundle_results: List[Dict[str, Any]]) -> str:
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
    if bundle_results:
        table_html += """
        <div><strong>Bundles Associated with this Regular Results</strong></div>
        <div style="padding-left: 20px; display: flex; flex-wrap: wrap; gap: 15px;">
        """
        for bundle in bundle_results:
            status = bundle.get("status", "Unknown")
            status_class = "history-success" if status == "SUCCESS" else "history-failure" if status == "FAILURE" else "history-aborted"
            bundle_timestamp = datetime.fromtimestamp(bundle["timestamp"], timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
            table_html += f"""
            <div class='history-square {status_class}' 
                onclick='window.open("{bundle["link"]}", "_blank")' 
                title='Status: {status} | Timestamp: {bundle_timestamp}'>
            </div>
            """
        table_html += "</div>"
        last_bundle_date = datetime.fromtimestamp(bundle_results[0]["timestamp"], timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        table_html += f"""
        <div><strong>Last Bundle Job Date: </strong>{last_bundle_date}</div>
        """
    table_html += "</div>"
    return table_html

def generate_test_matrix(ocp_data: Dict[str, List[Dict[str, Any]]]) -> str:
    sorted_ocp_versions = sorted(ocp_data.keys(), reverse=True)
    html_content = generate_html_header()
    for ocp_version in sorted_ocp_versions:
        results = ocp_data[ocp_version]
        regular_results = [
            r for r in results
            if ("bundle" not in r["gpu"].lower())
               and ("master" not in r["gpu"].lower())
               and (r.get("status") == "SUCCESS")
        ]
        bundle_results = [r for r in results if r not in regular_results]
        html_content += generate_regular_results_table(ocp_version, regular_results, bundle_results)
    html_content += """
    </body>
    </html>
    """
    return html_content

def main() -> None:
    parser = argparse.ArgumentParser(description="Generate test matrix HTML UI")
    parser.add_argument("--output_dir", required=True, help="Output directory where JSON is stored and HTML will be generated")
    parser.add_argument("--data_file", required=True, help="Name of the json file")
    parser.add_argument("--output_file", required=True, help="Name of the generated html file")
    args = parser.parse_args()

    # Construct path to JSON file in the output directory
    json_path = os.path.join(args.output_dir, args.data_file)

    with open(json_path, "r") as f:
        ocp_data = json.load(f)
    logger.info(f"Loaded JSON data with keys: {list(ocp_data.keys())} from {json_path}")

    html_content = generate_test_matrix(ocp_data)

    # Save the HTML in the same output directory
    output_path = os.path.join(args.output_dir, args.output_file)
    with open(output_path, "w") as f:
        f.write(html_content)
    logger.info(f"Matrix report generated: {output_path}")

if __name__ == "__main__":
    main()
