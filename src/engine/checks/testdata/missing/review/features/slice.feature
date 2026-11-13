Feature: Greetings

  Rule: R-1 A greeting names the person
    @R-1 @E-1
    Scenario: R-1/E-1 greet Ada
      When I ask for the greeting for "Ada"
      Then the text is "Hello, Ada"

  Rule: R-2 A greeting is not empty
    @R-2 @E-1
    Scenario: R-2/E-1 greet Bob
      When I ask for the greeting for "Bob"
      Then the text is not empty
