Feature: Notes

  Rule: R-1 Listing returns every note in creation order
    @R-1 @E-1
    Scenario: Notes are listed in creation order
      Given the notes "a" and "b" were created
      When the user lists the notes
      Then the notes are returned in the order "a", "b"

  Rule: R-2 A created note gets the next id, the first being 1
    @R-2 @E-2
    Scenario: The first note gets id 1
      When the user creates a note with the text "a"
      Then the response status is 201
      And the note has the id 1

    @R-2 @E-3
    Scenario: The second note gets id 2
      Given the notes "a" was created
      When the user creates a note with the text "b"
      Then the note has the id 2

  Rule: R-3 A note with empty text is refused
    @R-3 @E-4
    Scenario: Empty text is refused with a 400
      When the user creates a note with the text ""
      Then the response status is 400

  Rule: R-4 A note of more than 280 characters is refused
    @R-4 @E-5
    Scenario: A text of 281 characters is refused with a 400
      When the user creates a note with a text of 281 characters
      Then the response status is 400
