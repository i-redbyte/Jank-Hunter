package io.jankhunter.retrace;

import com.android.tools.r8.Diagnostic;
import com.android.tools.r8.DiagnosticsHandler;
import com.android.tools.r8.references.Reference;
import com.android.tools.r8.retrace.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.Arrays;

/** Versioned batch bridge. Mapping semantics belong exclusively to the pinned official R8 API. */
public final class Main {
  private static final int MAGIC = 0x4a485231; // JHR1
  private static final int MAX_ITEMS = 16384;
  private static final int MAX_STRING = 4 * 1024 * 1024;

  public static void main(String[] args) throws Exception {
    if (args.length != 1) throw new IllegalArgumentException("Expected mapping path");
    var in = new DataInputStream(new BufferedInputStream(System.in));
    var out = new DataOutputStream(new BufferedOutputStream(System.out));
    if (in.readInt() != MAGIC) throw new IOException("Unsupported Retrace protocol");
    int count = size(in.readInt(), MAX_ITEMS);
    var diagnostics = new DiagnosticsHandler() {
      @Override public void error(Diagnostic diagnostic) { throw failure(diagnostic); }
      // A mapping feature the pinned engine cannot interpret must not produce apparently exact output.
      @Override public void warning(Diagnostic diagnostic) { throw failure(diagnostic); }
      @Override public void info(Diagnostic diagnostic) {
        if (diagnostic instanceof RetraceUnknownMapVersionDiagnostic) throw failure(diagnostic);
      }
      private RuntimeException failure(Diagnostic diagnostic) {
        return new IllegalArgumentException("Official Retrace diagnostic: " + diagnostic.getDiagnosticMessage());
      }
    };
    var mapping = ProguardMappingSupplier.builder()
        .setProguardMapProducer(ProguardMapProducer.fromPath(Path.of(args[0])))
        .setLoadAllDefinitions(true).build();
    // Mapping identity is verified by the caller against the log SHA-256.
    // R8 internal hash headers are optional in legacy ProGuard mappings.
    // The command entry point validates mapping versions; StringRetrace alone does not.
    Retrace.run(RetraceCommand.builder(diagnostics).setMappingSupplier(mapping)
        .setVerifyMappingFileHash(false).setStackTrace(java.util.List.of())
        .setRetracedStackTraceConsumer(ignored -> {}).build());
    var stacks = StringRetrace.create(mapping, diagnostics, RetraceOptions.defaultRegularExpression(), false);
    var fields = mapping.createRetracer(diagnostics);
    out.writeInt(MAGIC);
    out.writeInt(count);
    for (int request = 0; request < count; request++) {
      int kind = in.readUnsignedByte();
      out.writeByte(kind);
      if (kind == 1) {
        var lines = Arrays.asList(readString(in).split("\n", -1));
        var result = stacks.retraceStackTrace(lines, RetraceStackTraceContext.empty());
        out.writeInt(size(result.getResult().size(), MAX_ITEMS));
        for (var group : result.getResult()) {
          out.writeBoolean(group.isAmbiguous());
          out.writeInt(size(group.size(), MAX_ITEMS));
          for (var candidate : group.getAmbiguousResult()) {
            out.writeInt(size(candidate.size(), MAX_ITEMS));
            for (var line : candidate.getResult()) writeString(out, line);
          }
        }
      } else if (kind == 2) {
        var owner = Reference.classFromTypeName(readString(in));
        var name = readString(in);
        var type = readString(in);
        var holder = fields.retraceClass(owner);
        var result = type.isEmpty() ? holder.lookupField(name) : holder.lookupField(name, Reference.typeFromDescriptor(type));
        out.writeBoolean(result.isAmbiguous());
        var alternatives = result.stream().limit(MAX_ITEMS + 1L).toList();
        out.writeInt(size(alternatives.size(), MAX_ITEMS));
        for (var alternative : alternatives) {
          var field = alternative.getField();
          out.writeBoolean(field.isKnown());
          writeString(out, field.getHolderClass().getTypeName());
          writeString(out, field.getFieldName());
          writeString(out, field.isKnown() ? field.asKnown().getFieldType().getDescriptor() : "");
        }
      } else {
        throw new IOException("Unsupported Retrace request kind: " + kind);
      }
    }
    if (in.read() != -1) throw new IOException("Trailing Retrace request data");
    out.flush();
  }

  private static int size(int value, int limit) throws IOException {
    if (value < 0 || value > limit) throw new IOException("Retrace protocol resource limit exceeded");
    return value;
  }

  private static String readString(DataInputStream in) throws IOException {
    int length = size(in.readInt(), MAX_STRING);
    byte[] value = in.readNBytes(length);
    if (value.length != length) throw new EOFException("Truncated Retrace request");
    return new String(value, StandardCharsets.UTF_8);
  }

  private static void writeString(DataOutputStream out, String value) throws IOException {
    byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
    out.writeInt(size(bytes.length, MAX_STRING));
    out.write(bytes);
  }
}
