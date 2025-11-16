# Prow CI Analyzer - Architecture & Design Philosophy

This document explains the design philosophy and architectural decisions behind the Prow CI Analyzer MCP server.

## Core Philosophy

The server acts as a bridge between AI assistants and OpenShift CI infrastructure, following a clear principle: **the server handles mechanical work, the AI handles reasoning**.

### Responsibilities

**Server (This Code):**
- ✅ Fetch files from GCS storage
- ✅ Parse structured formats (JSON, XML)
- ✅ Navigate complex directory structures
- ❌ Never interpret or filter failure data

**LLM (AI Assistant):**
- ✅ Understand logs and identify root causes
- ✅ Spot trends and anomalies
- ✅ Choose which tools to use and in what order
- ✅ Explain findings to users

## Tool Design: High-Level vs Low-Level

The server provides 15 tools in three categories, balancing efficiency with flexibility.

### High-Level Domain Tools

**What they do:**
- Encapsulate common workflows (check status, parse test results, find failures)
- Pre-parse structured data (JUnit XML, JSON metadata)
- Navigate complex structures (must-gather artifacts)
- Encode domain knowledge about Prow CI and OpenShift

**Benefits:**
- **Speed**: Answer common questions in 1-3 calls instead of 20+
- **Token efficiency**: Return parsed structures, not raw files
- **Focus**: Only relevant data, reducing noise
- **Reliability**: Tested against real CI patterns

**Trade-offs:**
- Fixed workflows can't handle every edge case
- May need updates when CI structure changes

### Low-Level Exploration Tools

**What they do:**
- Direct file system access (list any directory, fetch any file)
- No assumptions about structure or format
- Path helpers for manual navigation

**Benefits:**
- **Flexibility**: Handle unexpected structures and new patterns
- **Completeness**: Access any data, not just what high-level tools expose
- **Future-proof**: New CI patterns don't require code changes
- **Discovery**: Explore unknown territories

**Trade-offs:**
- More API calls needed for complex analysis
- LLM must understand file formats and structures
- Higher token usage for raw data

### Why Both?

**Example: Analyzing Test Failures**

*Without high-level tools:*
```text
1. List artifacts directory
2. List subdirectories
3. Look for XML files
4. Fetch XML file
5. Parse XML structure
6. Extract test names
7. Find failures
8. Extract error messages
= 20+ tool calls, high token usage
```

*With high-level tools:*
```text
1. Find JUnit files (returns all paths)
2. Get JUnit results (returns parsed failures)
= 2 tool calls, structured output
```

*When to use low-level:*
- Investigating non-standard artifacts
- Exploring new CI job structures
- Debugging unexpected failures
- Finding files not covered by high-level tools

### Must-Gather: A Special Case

OpenShift must-gather artifacts are massive (10,000+ files) and deeply nested. Without specialized tools, analyzing them would require hundreds of API calls and consume excessive tokens.

**Challenge:**
- Hierarchical structure: `namespace/pods/pod-name/container/logs/`
- Mixed content: YAML configs, logs, events, resource dumps
- Size: Gigabytes of data per must-gather

**Solution:**
- Specialized search tools that understand the structure
- Pattern matching for finding relevant files quickly
- Targeted fetching of specific pod logs or config files
- Pre-filtering to surface important data

**Limitation:**
Currently only handles extracted directories, not archives. This trades initial setup time (must-gather needs extraction) for analysis efficiency (no need to handle tar streams).

## Architecture Principles

### Separation of Concerns

The codebase is organized into distinct layers:

1. **Protocol Layer**: Handles MCP communication
2. **Tool Layer**: Defines available tools and validates requests
3. **Business Logic Layer**: Implements CI-specific operations
4. **Data Access Layer**: Manages GCS API interactions
5. **Parsing Layer**: Converts raw data into structured formats

Each layer has clear boundaries and minimal coupling.

### Modularity

The system is designed for independent testing and evolution:
- Parsers can be used standalone
- GCS client is infrastructure-agnostic
- Business logic doesn't know about MCP protocol
- Configuration is centralized

### Error Handling Philosophy

**Server responsibilities:**
- Validate tool parameters
- Handle GCS API errors gracefully
- Return errors as structured JSON
- Never crash on invalid input

**LLM responsibilities:**
- Interpret error messages
- Retry with corrected parameters
- Choose alternative approaches
- Explain issues to users

## Extensibility

### Supporting New File Formats

The parsing layer is designed to be extended:
- Add new parsers without touching existing code
- Parsers return structured data in consistent formats
- Each parser is independent and testable

**When to add a parser:**
- New test frameworks (e.g., pytest, Go test)
- CI-specific metadata formats
- Custom log formats that appear frequently

### Supporting New CI Systems

While built for Prow/OpenShift CI, the architecture supports other systems:
- GCS client is infrastructure-neutral
- Path construction is template-based
- Tool schemas can be extended or replaced

**What would change:**
- Path templates (currently `pr-logs/pull/...`)
- Job discovery logic
- Metadata file locations

**What wouldn't change:**
- MCP protocol layer
- Parsers (JUnit, JSON)
- Low-level exploration tools

### Adding New High-Level Tools

Consider adding a high-level tool when:
- A workflow requires 10+ low-level calls
- The same pattern appears in multiple analyses
- Domain knowledge can reduce complexity
- Token usage would be significantly reduced

**Keep tools focused:**
- Single responsibility
- Clear success/failure semantics
- Consistent parameter patterns

## Performance Design

### Token Efficiency

The server minimizes token usage by:
- Parsing structured data server-side
- Returning summaries, not full dumps
- Pre-filtering large datasets
- Using hierarchical navigation (overview → details)

### API Call Efficiency

High-level tools batch operations:
- Single call returns multiple related pieces
- Directory listings include file metadata
- Parsers return both summary and details

### Caching Strategy

Currently implemented:
- Repository configuration cache (startup)

Future opportunities:
- Job metadata (expensive to refetch)
- Directory listings (relatively stable)
- Build status (immutable once complete)

## Design Trade-offs

### Current Decisions

#### Synchronous I/O
- Simpler implementation
- Easier to debug
- Trade-off: Sequential fetches slower for batch operations

#### No persistent caching
- Simpler deployment (no cache invalidation)
- Always fresh data
- Trade-off: Repeated fetches for same data

#### Extracted must-gather only
- Simpler file access
- No tar/gzip handling needed
- Trade-off: Requires pre-extraction step

#### Server-side parsing
- Lower token usage
- Faster for common cases
- Trade-off: Less flexible than raw data

### Why These Trade-offs?

Each prioritizes **simplicity and reliability** over performance optimization. The system is fast enough for interactive use while remaining easy to understand and maintain.

## Future Directions

### Potential Capabilities

#### Historical Analysis
- Track test flakiness trends over time
- Compare success rates across PRs
- Identify systemic issues

#### Cross-Repository Insights
- Find similar failures across projects
- Share debugging knowledge
- Identify infrastructure issues

#### Smart Caching
- Remember immutable build data
- Reduce redundant fetches
- Speed up repeat analyses

#### Advanced Streaming
- Handle logs too large for memory
- Parallel GCS operations
- Progressive result delivery

### Extension Philosophy

New features should:
- Maintain the high-level/low-level balance
- Let the LLM reason, server handle mechanics
- Add capability without adding complexity
- Be independently testable

## Related Resources

- [MCP Protocol Specification](https://modelcontextprotocol.io/)
- [OpenShift CI Documentation](https://docs.ci.openshift.org/)
- [Prow Documentation](https://docs.prow.k8s.io/)
