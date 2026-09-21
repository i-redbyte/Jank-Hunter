package fixture;

public class Overloads {
    public int first(int value) {
        return value + 1;
    }

    public int second(String value) {
        return value.length();
    }

    public long firstField;
    public String secondField;
}
