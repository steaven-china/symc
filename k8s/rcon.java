// RCON client in pure Java - no external deps
import java.net.Socket;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;

public class Rcon {
    public static void main(String[] args) throws Exception {
        String host = "127.0.0.1";
        int port = 25575;
        String password = "symc";
        String command = String.join(" ", args);

        Socket sock = new Socket(host, port);
        sock.setSoTimeout(5000);
        DataOutputStream out = new DataOutputStream(sock.getOutputStream());
        DataInputStream in = new DataInputStream(sock.getInputStream());

        // Login: send + read 1 packet
        sendPacket(out, 0, 3, password);
        recvPacket(in);
        // Some servers send 2 packets after login - try non-blocking
        // but our recvPacket uses blocking readNBytes, so skip

        // Command: send + read 1 packet
        sendPacket(out, 1, 2, command);
        byte[] resp = recvPacket(in);
        String s = new String(resp, StandardCharsets.UTF_8);
        System.out.println("RESP:" + s);

        sock.close();
    }

    static void sendPacket(DataOutputStream out, int reqId, int type, String payload) throws Exception {
        byte[] payloadBytes = payload.getBytes(StandardCharsets.UTF_8);
        int length = 4 + 4 + payloadBytes.length + 2;
        ByteBuffer buf = ByteBuffer.allocate(4 + length).order(ByteOrder.LITTLE_ENDIAN);
        buf.putInt(length);
        buf.putInt(reqId);
        buf.putInt(type);
        buf.put(payloadBytes);
        buf.put((byte) 0);
        buf.put((byte) 0);
        out.write(buf.array());
        out.flush();
    }

    static byte[] recvPacket(DataInputStream in) throws Exception {
        byte[] lenBytes = in.readNBytes(4);
        if (lenBytes.length < 4) return new byte[0];
        int length = ByteBuffer.wrap(lenBytes).order(ByteOrder.LITTLE_ENDIAN).getInt();
        byte[] data = in.readNBytes(length);
        if (data.length < length) {
            byte[] more = in.readNBytes(length - data.length);
            byte[] full = new byte[data.length + more.length];
            System.arraycopy(data, 0, full, 0, data.length);
            System.arraycopy(more, 0, full, data.length, more.length);
            data = full;
        }
        int payloadLen = length - 4 - 4 - 2;
        if (payloadLen <= 0) return new byte[0];
        byte[] payload = new byte[payloadLen];
        System.arraycopy(data, 8, payload, 0, payloadLen);
        return payload;
    }
}
