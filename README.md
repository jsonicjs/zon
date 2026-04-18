# @jsonic/zon

A [Jsonic](https://jsonic.senecajs.org) syntax plugin that parses
[Zig Object Notation (ZON)](https://ziglang.org/documentation/master/#ZON)
text into objects, arrays, and scalar values. Available for
TypeScript and Go.

ZON is the data format used for Zig `build.zig.zon` manifests and
similar configuration files. It is based on Zig anonymous struct
literals, and looks like this:

```zon
.{
    .name = "example",
    .version = "0.0.1",
    .dependencies = .{
        .foo = .{
            .url = "https://example.com/foo.tar.gz",
            .hash = "1220deadbeef",
        },
    },
    .paths = .{
        "build.zig",
        "src",
    },
}
```

## Quick example

**TypeScript**

```typescript
import { Jsonic } from 'jsonic'
import { Zon } from '@jsonic/zon'

const parse = Jsonic.make().use(Zon)

parse('.{ .name = "Alice", .age = 30 }')
// { name: 'Alice', age: 30 }

parse('.{ 1, 2, 3 }')
// [1, 2, 3]
```

**Go**

```go
import zon "github.com/jsonicjs/zon/go"
import jsonic "github.com/jsonicjs/jsonic/go"

j := jsonic.Make()
j.UseDefaults(zon.Zon, zon.Defaults)

result, _ := j.Parse(`.{ .name = "Alice", .age = 30 }`)
// map[string]any{"name": "Alice", "age": 30}
```

## Supported syntax

- Anonymous struct literals: `.{ .field = value, ... }`
- Tuple / array literals: `.{ value, value, ... }`
- Field names: `.identifier`
- Enum literals (used as values): `.identifier` (parsed as bare strings)
- Strings: `"..."` with Zig escape sequences (`\n`, `\r`, `\t`, `\\`, `\"`, `\'`)
- Multi-line strings: consecutive lines starting with `\\`
- Numbers: decimal, `0x` hex, `0o` octal, `0b` binary, with `_` separators
- Character literals: `'x'`, `'\n'`, `'\x41'`, `'\u{1F600}'`
- Keywords: `true`, `false`, `null`
- Line comments: `// ...`
- Trailing commas allowed

## Options

| Option         | Default | Description                                          |
| -------------- | ------- | ---------------------------------------------------- |
| `charAsNumber` | `false` | Parse character literals as numeric code points.     |
| `enumTag`      | `null`  | If set, wrap enum literals in `{ [enumTag]: name }`. |

## License

Copyright (c) 2025 Richard Rodger and other contributors,
[MIT License](LICENSE).
